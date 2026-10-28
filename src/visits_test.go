package main

import (
	"net/http"
	"net/http/httptest"
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
	}), false, "correct horse battery staple", store)

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
		path string
		want bool
	}{
		{path: "/", want: true},
		{path: "/healthz", want: true},
		{path: "/visits", want: false},
		{path: "/visits.css", want: false},
	}
	for _, tt := range tests {
		if got := shouldRecordVisit(tt.path); got != tt.want {
			t.Errorf("shouldRecordVisit(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestVisitsEndpointDisabledWithoutToken(t *testing.T) {
	t.Parallel()

	handler := newRouterWithConfig(http.FS(fstest.MapFS{
		"index.html": {Data: []byte("hello")},
	}), false, "", newVisitStore())
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
	}), false, "", newVisitStore())
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
