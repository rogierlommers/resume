package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestVisitStoreSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	store := newVisitStore()
	emptyEstimate := store.snapshot(now).EstimatedBytes
	store.record(visit{
		Time: now.AddDate(0, 0, -7), Path: "/expired", RemoteIP: "192.0.2.1",
	})
	store.record(visit{
		Time: now.AddDate(0, 0, -6), Path: "/", RemoteIP: "192.0.2.2", Referer: "https://search.example/?secret=value",
	})
	store.record(visit{
		Time: now, Path: "/", RemoteIP: "192.0.2.2", UserAgent: "browser",
	})
	store.record(visit{
		Time: now, Path: "/resume.pdf", RemoteIP: "192.0.2.3", UserAgent: "browser",
	})

	snapshot := store.snapshot(now)
	if snapshot.Total != 3 {
		t.Fatalf("total = %d, want 3", snapshot.Total)
	}
	if snapshot.Unique != 2 {
		t.Errorf("unique = %d, want 2", snapshot.Unique)
	}
	if len(snapshot.TopPaths) == 0 || snapshot.TopPaths[0] != (namedCount{Name: "/", Count: 2}) {
		t.Errorf("top paths = %#v, want / with count 2 first", snapshot.TopPaths)
	}
	if !containsCount(snapshot.TopReferrers, namedCount{Name: "https://search.example/", Count: 1}) {
		t.Errorf("top referrers = %#v, want sanitized referrer", snapshot.TopReferrers)
	}
	if len(snapshot.Recent) != 4 || snapshot.Recent[0].Path != "/resume.pdf" {
		t.Errorf("recent visits are not newest-first: %#v", snapshot.Recent)
	}
	if snapshot.EstimatedBytes <= emptyEstimate {
		t.Errorf("estimated bytes = %d, want more than empty store estimate %d", snapshot.EstimatedBytes, emptyEstimate)
	}
	if len(snapshot.ChartDays) != aggregateDays {
		t.Fatalf("chart days = %d, want %d", len(snapshot.ChartDays), aggregateDays)
	}
	if snapshot.ChartDays[aggregateDays-8].Visits != 1 {
		t.Errorf("chart visits seven days ago = %d, want 1", snapshot.ChartDays[aggregateDays-8].Visits)
	}
	if snapshot.ChartDays[aggregateDays-1].Visits != 2 || snapshot.ChartPeak != 2 {
		t.Errorf("chart current day = %d and peak = %d, want 2 and 2", snapshot.ChartDays[aggregateDays-1].Visits, snapshot.ChartPeak)
	}
	if snapshot.ChartDays[aggregateDays-1].ValueY >= 210 || snapshot.ChartDays[aggregateDays-1].Label != "Oct 28" {
		t.Errorf("current chart day is missing readable value positioning or date: %#v", snapshot.ChartDays[aggregateDays-1])
	}
}

func TestFormatBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		bytes uint64
		want  string
	}{
		{bytes: 512, want: "512 B"},
		{bytes: 1536, want: "1.5 KiB"},
		{bytes: 2 * 1024 * 1024, want: "2.0 MiB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.bytes); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestVisitStoreCapsRecentVisits(t *testing.T) {
	t.Parallel()

	store := newVisitStore()
	now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	for index := 0; index < maxRecentVisits+5; index++ {
		store.record(visit{Time: now, Path: "/"})
	}

	snapshot := store.snapshot(now)
	if len(snapshot.Recent) != maxRecentVisits {
		t.Fatalf("recent visits = %d, want %d", len(snapshot.Recent), maxRecentVisits)
	}
}

func TestSQLiteVisitStorePersistsAndExpiresVisits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "visits.db")
	store, err := newSQLiteVisitStore(path)
	if err != nil {
		t.Fatalf("open SQLite visit store: %v", err)
	}
	visits := []visit{
		{Time: now.AddDate(0, 0, -30), Path: "/expired", RemoteIP: "192.0.2.1"},
		{Time: now.AddDate(0, 0, -6), Path: "/", RemoteIP: "192.0.2.2", Referer: "https://search.example/?secret=value"},
		{Time: now, Path: "/", RemoteIP: "192.0.2.2", UserAgent: "browser"},
		{Time: now, Path: "/resume.pdf", RemoteIP: "192.0.2.3", UserAgent: "browser"},
	}
	for _, value := range visits {
		if err := store.Record(value); err != nil {
			t.Fatalf("record visit: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close SQLite visit store: %v", err)
	}

	store, err = newSQLiteVisitStore(path)
	if err != nil {
		t.Fatalf("reopen SQLite visit store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	snapshot, err := store.Snapshot(now)
	if err != nil {
		t.Fatalf("snapshot SQLite visit store: %v", err)
	}
	if snapshot.Total != 3 || snapshot.Unique != 2 {
		t.Errorf("total = %d and unique = %d, want 3 and 2", snapshot.Total, snapshot.Unique)
	}
	if len(snapshot.TopPaths) == 0 || snapshot.TopPaths[0] != (namedCount{Name: "/", Count: 2}) {
		t.Errorf("top paths = %#v, want / with count 2 first", snapshot.TopPaths)
	}
	if !containsCount(snapshot.TopReferrers, namedCount{Name: "https://search.example/", Count: 1}) {
		t.Errorf("top referrers = %#v, want sanitized referrer", snapshot.TopReferrers)
	}
	if len(snapshot.Recent) != 3 || snapshot.Recent[0].Path != "/resume.pdf" {
		t.Errorf("recent visits = %#v, want three retained visits newest-first", snapshot.Recent)
	}
	if snapshot.ChartDays[aggregateDays-1].Visits != 2 || snapshot.ChartPeak != 2 {
		t.Errorf("chart current day = %d and peak = %d, want 2 and 2", snapshot.ChartDays[aggregateDays-1].Visits, snapshot.ChartPeak)
	}
	if snapshot.EstimatedBytes == 0 || snapshot.StorageLabel != "SQLite database size" {
		t.Errorf("storage bytes = %d and label = %q, want a SQLite size", snapshot.EstimatedBytes, snapshot.StorageLabel)
	}
}

func TestSQLiteVisitStoreRejectsMissingDirectory(t *testing.T) {
	t.Parallel()

	store, err := newSQLiteVisitStore(filepath.Join(t.TempDir(), "missing", "visits.db"))
	if err == nil {
		_ = store.Close()
		t.Fatal("opening a database in a missing directory succeeded")
	}
}

func TestVisitsEndpointAuthenticationAndEscaping(t *testing.T) {
	t.Parallel()

	store := newVisitStore()
	store.record(visit{
		Time:      time.Now(),
		Path:      "/resume.pdf",
		RemoteIP:  "203.0.113.9",
		UserAgent: `<script>alert("xss")</script>`,
	})
	handler := newRouterWithConfig(http.FS(fstest.MapFS{
		"index.html": {Data: []byte("hello")},
	}), false, false, "correct horse battery staple", store)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/visits", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}
	if unauthorized.Header().Get("WWW-Authenticate") == "" {
		t.Error("WWW-Authenticate header is missing")
	}

	request := httptest.NewRequest(http.MethodGet, "/visits", nil)
	request.SetBasicAuth("viewer", "correct horse battery staple")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want %d", authorized.Code, http.StatusOK)
	}
	if authorized.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", authorized.Header().Get("Cache-Control"))
	}
	body := authorized.Body.String()
	if !strings.Contains(body, "Last 30 days") || !strings.Contains(body, "Last 1,000 visits") || !strings.Contains(body, "Estimated analytics memory") || !strings.Contains(body, "203.0.113.9") {
		t.Errorf("dashboard is missing expected visit data")
	}
	if strings.Contains(body, "Visits by day") {
		t.Error("dashboard still contains the removed visits-by-day section")
	}
	if !strings.Contains(body, `class="visit-count"`) || !strings.Contains(body, `class="date-label"`) {
		t.Error("chart does not label every bar with a visit count and date")
	}
	if strings.Contains(body, `<script>alert("xss")</script>`) {
		t.Error("dashboard contains an unescaped user agent")
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Error("dashboard contains a rejected template value")
	}
	if got := store.snapshot(time.Now()).Total; got != 1 {
		t.Errorf("total after dashboard requests = %d, want 1 because dashboard views are excluded", got)
	}
}

func TestShouldRecordVisitExcludesDashboard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path            string
		status          int
		record404Visits bool
		want            bool
	}{
		{path: "/", status: http.StatusOK, want: true},
		{path: "/missing", status: http.StatusNotFound, want: false},
		{path: "/missing", status: http.StatusNotFound, record404Visits: true, want: true},
		{path: "/healthz", status: http.StatusOK, want: false},
		{path: "/visits", status: http.StatusOK, want: false},
		{path: "/visits.css", status: http.StatusOK, want: false},
		{path: "/favicon.ico", status: http.StatusOK, want: false},
	}
	for _, tt := range tests {
		if got := shouldRecordVisit(tt.path, tt.status, tt.record404Visits); got != tt.want {
			t.Errorf("shouldRecordVisit(%q, %d, %v) = %v, want %v", tt.path, tt.status, tt.record404Visits, got, tt.want)
		}
	}
}

func TestRouterOptionallyRecords404Visits(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name            string
		record404Visits bool
		wantTotal       int
	}{
		{name: "disabled", record404Visits: false, wantTotal: 0},
		{name: "enabled", record404Visits: true, wantTotal: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newVisitStore()
			handler := newRouterWithConfig(http.FS(fstest.MapFS{
				"index.html": {Data: []byte("hello")},
			}), false, tt.record404Visits, "token", store)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/missing", nil))

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
			}
			if got := store.snapshot(time.Now()).Total; got != tt.wantTotal {
				t.Fatalf("recorded visits = %d, want %d", got, tt.wantTotal)
			}
		})
	}
}

func TestVisitsEndpointDisabledWithoutToken(t *testing.T) {
	t.Parallel()

	handler := newRouterWithConfig(http.FS(fstest.MapFS{
		"index.html": {Data: []byte("hello")},
	}), false, false, "", newVisitStore())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/visits", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestVisitsStylesheetUsesFourColumnOverviewWithoutCaching(t *testing.T) {
	t.Parallel()

	handler := newRouterWithConfig(http.FS(fstest.MapFS{
		"index.html": {Data: []byte("hello")},
	}), false, false, "", newVisitStore())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/visits.css", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if !strings.Contains(recorder.Body.String(), "grid-template-columns: repeat(4, minmax(0, 1fr))") {
		t.Error("stylesheet does not use four overview columns")
	}
}

func containsCount(counts []namedCount, want namedCount) bool {
	for _, count := range counts {
		if count == want {
			return true
		}
	}
	return false
}
