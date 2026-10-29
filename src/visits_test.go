package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
	}), false, false, "viewer", "correct horse battery staple", store)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/visits", nil))
	if unauthorized.Code != http.StatusFound {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusFound)
	}
	if unauthorized.Header().Get("Location") != "/visits/login" {
		t.Errorf("unauthorized location = %q, want /visits/login", unauthorized.Header().Get("Location"))
	}

	loginPage := httptest.NewRecorder()
	handler.ServeHTTP(loginPage, httptest.NewRequest(http.MethodGet, "/visits/login", nil))
	if loginPage.Code != http.StatusOK || !strings.Contains(loginPage.Body.String(), `autocomplete="current-password"`) {
		t.Fatalf("login page status = %d or password-manager fields are missing", loginPage.Code)
	}

	wrongLogin := httptest.NewRecorder()
	wrongRequest := httptest.NewRequest(http.MethodPost, "/visits/login", strings.NewReader(url.Values{
		"username": {"viewer"}, "password": {"wrong"},
	}.Encode()))
	wrongRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(wrongLogin, wrongRequest)
	if wrongLogin.Code != http.StatusUnauthorized || !strings.Contains(wrongLogin.Body.String(), "username or password is incorrect") {
		t.Fatalf("wrong login status = %d and body = %q", wrongLogin.Code, wrongLogin.Body.String())
	}
	wrongUsername := httptest.NewRecorder()
	wrongUsernameRequest := httptest.NewRequest(http.MethodPost, "/visits/login", strings.NewReader(url.Values{
		"username": {"someone-else"}, "password": {"correct horse battery staple"},
	}.Encode()))
	wrongUsernameRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(wrongUsername, wrongUsernameRequest)
	if wrongUsername.Code != http.StatusUnauthorized {
		t.Fatalf("wrong username status = %d, want %d", wrongUsername.Code, http.StatusUnauthorized)
	}

	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/visits/login", strings.NewReader(url.Values{
		"username": {"viewer"}, "password": {"correct horse battery staple"},
	}.Encode()))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusSeeOther || login.Header().Get("Location") != "/visits" {
		t.Fatalf("login status = %d and location = %q, want redirect to /visits", login.Code, login.Header().Get("Location"))
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}
	session := cookies[0]
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/visits" || session.MaxAge <= 0 {
		t.Errorf("session cookie is missing security attributes: %#v", session)
	}

	request := httptest.NewRequest(http.MethodGet, "/visits", nil)
	request.AddCookie(session)
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

	logoutRequest := httptest.NewRequest(http.MethodPost, "/visits/logout", nil)
	logoutRequest.AddCookie(session)
	logout := httptest.NewRecorder()
	handler.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusSeeOther || logout.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout did not clear the session: status %d, cookies %#v", logout.Code, logout.Result().Cookies())
	}
	afterLogoutRequest := httptest.NewRequest(http.MethodGet, "/visits", nil)
	afterLogoutRequest.AddCookie(session)
	afterLogout := httptest.NewRecorder()
	handler.ServeHTTP(afterLogout, afterLogoutRequest)
	if afterLogout.Code != http.StatusFound {
		t.Fatalf("reusing signed-out session status = %d, want %d", afterLogout.Code, http.StatusFound)
	}
}

func TestVisitsSessionExpiresAndUsesSecureCookieBehindHTTPSProxy(t *testing.T) {
	t.Parallel()

	auth := newVisitsAuth("viewer", "secret")
	now := time.Date(2026, time.October, 29, 10, 0, 0, 0, time.UTC)
	request := httptest.NewRequest(http.MethodPost, "/visits/login", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	if err := auth.startSession(recorder, request, now); err != nil {
		t.Fatalf("start session: %v", err)
	}
	cookie := recorder.Result().Cookies()[0]
	if !cookie.Secure {
		t.Error("session cookie is not Secure for a proxied HTTPS request")
	}
	authorized := httptest.NewRequest(http.MethodGet, "/visits", nil)
	authorized.AddCookie(cookie)
	if !auth.authorized(authorized, now.Add(visitsSessionTTL-time.Second)) {
		t.Error("session expired before its configured lifetime")
	}
	if auth.authorized(authorized, now.Add(visitsSessionTTL)) {
		t.Error("session remains valid at its expiry time")
	}
}

func TestVisitsSessionsAreBounded(t *testing.T) {
	t.Parallel()

	auth := newVisitsAuth("viewer", "secret")
	now := time.Date(2026, time.October, 29, 10, 0, 0, 0, time.UTC)
	request := httptest.NewRequest(http.MethodPost, "/visits/login", nil)
	for index := 0; index < maxVisitsSessions+1; index++ {
		if err := auth.startSession(httptest.NewRecorder(), request, now.Add(time.Duration(index)*time.Second)); err != nil {
			t.Fatalf("start session %d: %v", index, err)
		}
	}
	if len(auth.sessions) != maxVisitsSessions {
		t.Fatalf("retained sessions = %d, want %d", len(auth.sessions), maxVisitsSessions)
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
		{path: "/visits/login", status: http.StatusOK, want: false},
		{path: "/visits/logout", status: http.StatusSeeOther, want: false},
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
			}), false, tt.record404Visits, "viewer", "token", store)

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

func TestVisitsEndpointDisabledWithoutCredentials(t *testing.T) {
	t.Parallel()

	for _, credentials := range []struct {
		name     string
		username string
		password string
	}{
		{name: "both missing"},
		{name: "username missing", password: "token"},
		{name: "token missing", username: "viewer"},
	} {
		t.Run(credentials.name, func(t *testing.T) {
			handler := newRouterWithConfig(http.FS(fstest.MapFS{
				"index.html": {Data: []byte("hello")},
			}), false, false, credentials.username, credentials.password, newVisitStore())
			for _, path := range []string{"/visits", "/visits/login"} {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
				if recorder.Code != http.StatusServiceUnavailable {
					t.Errorf("%s status = %d, want %d", path, recorder.Code, http.StatusServiceUnavailable)
				}
			}
		})
	}
}

func TestVisitsStylesheetUsesFourColumnOverviewWithoutCaching(t *testing.T) {
	t.Parallel()

	handler := newRouterWithConfig(http.FS(fstest.MapFS{
		"index.html": {Data: []byte("hello")},
	}), false, false, "", "", newVisitStore())
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
