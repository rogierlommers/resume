package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
)

const (
	maxRecentVisits   = 1000
	aggregateDays     = 30
	maxDimensionKeys  = 1000
	maxUniquePerDay   = 100000
	maxPathLength     = 512
	maxHeaderLength   = 1024
	visitsSessionTTL  = 12 * time.Hour
	visitsCookieName  = "visits_session"
	maxVisitsSessions = 32
)

type visit struct {
	Time          time.Time
	Method        string
	Path          string
	Status        int
	Duration      time.Duration
	ResponseBytes int
	RemoteIP      string
	Host          string
	Protocol      string
	UserAgent     string
	Referer       string
}

type visitDay struct {
	total          int
	unique         map[string]struct{}
	uniqueOverflow bool
	paths          map[string]int
	referrers      map[string]int
	userAgents     map[string]int
}

type visitStore struct {
	mu     sync.Mutex
	days   map[string]*visitDay
	recent []visit
}

type visitsStorage interface {
	Record(visit) error
	Snapshot(time.Time) (visitSnapshot, error)
}

type visitsAuth struct {
	usernameHash [sha256.Size]byte
	passwordHash [sha256.Size]byte
	configured   bool
	mu           sync.Mutex
	sessions     map[[sha256.Size]byte]time.Time
}

type namedCount struct {
	Name  string
	Count int
}

type chartDay struct {
	Date    string
	Label   string
	Visits  int
	X       int
	CenterX int
	Y       int
	ValueY  int
	Height  int
}

type visitSnapshot struct {
	GeneratedAt     time.Time
	Since           time.Time
	Total           int
	Unique          int
	UniqueIsMinimum bool
	TopPaths        []namedCount
	TopReferrers    []namedCount
	TopUserAgents   []namedCount
	Recent          []visit
	EstimatedBytes  uint64
	StorageLabel    string
	StorageNote     string
	ChartDays       []chartDay
	ChartPeak       int
}

func newVisitStore() *visitStore {
	return &visitStore{days: make(map[string]*visitDay)}
}

func (s *visitStore) Record(v visit) error {
	s.record(v)
	return nil
}

func (s *visitStore) Snapshot(now time.Time) (visitSnapshot, error) {
	return s.snapshot(now), nil
}

func (s *visitStore) record(v visit) {
	v = normalizeVisit(v)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.purge(v.Time)
	key := dayKey(v.Time)
	day := s.days[key]
	if day == nil {
		day = &visitDay{
			unique:     make(map[string]struct{}),
			paths:      make(map[string]int),
			referrers:  make(map[string]int),
			userAgents: make(map[string]int),
		}
		s.days[key] = day
	}
	day.total++
	if v.RemoteIP != "" {
		if _, exists := day.unique[v.RemoteIP]; exists {
			// Already counted.
		} else if len(day.unique) < maxUniquePerDay {
			day.unique[v.RemoteIP] = struct{}{}
		} else {
			day.uniqueOverflow = true
		}
	}
	incrementBounded(day.paths, displayValue(v.Path, "(no path)"))
	incrementBounded(day.referrers, displayValue(v.Referer, "(direct)"))
	incrementBounded(day.userAgents, displayValue(v.UserAgent, "(unknown)"))

	s.recent = append(s.recent, v)
	if len(s.recent) > maxRecentVisits {
		copy(s.recent, s.recent[len(s.recent)-maxRecentVisits:])
		s.recent = s.recent[:maxRecentVisits]
	}
}

func normalizeVisit(v visit) visit {
	v.Path = truncate(v.Path, maxPathLength)
	v.Host = truncate(v.Host, maxHeaderLength)
	v.Protocol = truncate(v.Protocol, maxHeaderLength)
	v.UserAgent = truncate(v.UserAgent, maxHeaderLength)
	v.Referer = truncate(sanitizeReferer(v.Referer), maxHeaderLength)
	v.RemoteIP = truncate(v.RemoteIP, maxHeaderLength)
	return v
}

func (s *visitStore) snapshot(now time.Time) visitSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.purge(now)
	since := startOfUTCDay(now).AddDate(0, 0, -6)
	snapshot := visitSnapshot{
		GeneratedAt:  now.UTC(),
		Since:        since,
		StorageLabel: "Estimated analytics memory",
		StorageNote:  "Data resets when the server restarts.",
	}
	unique := make(map[string]struct{})
	paths := make(map[string]int)
	referrers := make(map[string]int)
	userAgents := make(map[string]int)

	for offset := 0; offset < 7; offset++ {
		date := since.AddDate(0, 0, offset)
		day := s.days[dayKey(date)]
		if day != nil {
			snapshot.Total += day.total
			for ip := range day.unique {
				unique[ip] = struct{}{}
			}
			snapshot.UniqueIsMinimum = snapshot.UniqueIsMinimum || day.uniqueOverflow
			mergeCounts(paths, day.paths)
			mergeCounts(referrers, day.referrers)
			mergeCounts(userAgents, day.userAgents)
		}
	}
	snapshot.Unique = len(unique)
	snapshot.TopPaths = topCounts(paths, 10)
	snapshot.TopReferrers = topCounts(referrers, 10)
	snapshot.TopUserAgents = topCounts(userAgents, 10)
	snapshot.EstimatedBytes = s.estimatedBytes()
	snapshot.ChartDays, snapshot.ChartPeak = s.chartDays(now)

	snapshot.Recent = make([]visit, len(s.recent))
	for index := range s.recent {
		snapshot.Recent[len(s.recent)-1-index] = s.recent[index]
	}
	return snapshot
}

func (s *visitStore) chartDays(now time.Time) ([]chartDay, int) {
	const (
		left       = 32
		barSpacing = 28
		baseline   = 210
		maxHeight  = 170
	)

	since := startOfUTCDay(now).AddDate(0, 0, -(aggregateDays - 1))
	counts := make([]int, aggregateDays)
	peak := 0
	for offset := range aggregateDays {
		if day := s.days[dayKey(since.AddDate(0, 0, offset))]; day != nil {
			counts[offset] = day.total
			if day.total > peak {
				peak = day.total
			}
		}
	}

	days := make([]chartDay, 0, aggregateDays)
	for offset, count := range counts {
		date := since.AddDate(0, 0, offset)
		height := 0
		if count > 0 && peak > 0 {
			height = count * maxHeight / peak
			if height < 2 {
				height = 2
			}
		}
		valueY := baseline - height - 6
		if valueY < 14 {
			valueY = 14
		}
		days = append(days, chartDay{
			Date:    date.Format("2006-01-02"),
			Label:   date.Format("Jan 2"),
			Visits:  count,
			X:       left + offset*barSpacing,
			CenterX: left + offset*barSpacing + 9,
			Y:       baseline - height,
			ValueY:  valueY,
			Height:  height,
		})
	}
	return days, peak
}

// estimatedBytes approximates the memory retained by the analytics store. Map
// bucket and allocator overhead are implementation details, so each map and
// entry receives a conservative allowance rather than claiming exact usage.
func (s *visitStore) estimatedBytes() uint64 {
	const (
		mapBaseOverhead  = 64
		mapEntryOverhead = 16
	)

	size := uint64(unsafe.Sizeof(*s))
	size += uint64(cap(s.recent)) * uint64(unsafe.Sizeof(visit{}))
	for _, recent := range s.recent {
		size += visitStringBytes(recent)
	}

	size += mapBaseOverhead
	for key, day := range s.days {
		size += uint64(len(key)) + uint64(unsafe.Sizeof(key)) + uint64(unsafe.Sizeof(day)) + mapEntryOverhead
		size += uint64(unsafe.Sizeof(*day))
		size += estimateStringSet(day.unique, mapBaseOverhead, mapEntryOverhead)
		size += estimateStringCounts(day.paths, mapBaseOverhead, mapEntryOverhead)
		size += estimateStringCounts(day.referrers, mapBaseOverhead, mapEntryOverhead)
		size += estimateStringCounts(day.userAgents, mapBaseOverhead, mapEntryOverhead)
	}
	return size
}

func visitStringBytes(value visit) uint64 {
	return uint64(len(value.Method) + len(value.Path) + len(value.RemoteIP) + len(value.Host) + len(value.Protocol) + len(value.UserAgent) + len(value.Referer))
}

func estimateStringSet(values map[string]struct{}, baseOverhead, entryOverhead uint64) uint64 {
	size := baseOverhead
	for value := range values {
		size += uint64(len(value)) + uint64(unsafe.Sizeof(value)) + entryOverhead
	}
	return size
}

func estimateStringCounts(values map[string]int, baseOverhead, entryOverhead uint64) uint64 {
	size := baseOverhead
	for value := range values {
		size += uint64(len(value)) + uint64(unsafe.Sizeof(value)) + uint64(unsafe.Sizeof(int(0))) + entryOverhead
	}
	return size
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return strconv.FormatUint(bytes, 10) + " B"
	}
	divisor := uint64(unit)
	unitIndex := 0
	for amount := bytes / unit; amount >= unit && unitIndex < 3; amount /= unit {
		divisor *= unit
		unitIndex++
	}
	return strconv.FormatFloat(float64(bytes)/float64(divisor), 'f', 1, 64) + " " + []string{"KiB", "MiB", "GiB", "TiB"}[unitIndex]
}

func (s *visitStore) purge(now time.Time) {
	cutoff := startOfUTCDay(now).AddDate(0, 0, -(aggregateDays - 1))
	for key := range s.days {
		date, err := time.Parse("2006-01-02", key)
		if err != nil || date.Before(cutoff) {
			delete(s.days, key)
		}
	}
}

func startOfUTCDay(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func dayKey(value time.Time) string {
	return value.UTC().Format("2006-01-02")
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func sanitizeReferer(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		if index := strings.IndexAny(value, "?#"); index >= 0 {
			return value[:index]
		}
		return value
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil
	return parsed.String()
}

func displayValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func incrementBounded(counts map[string]int, value string) {
	if _, exists := counts[value]; exists || len(counts) < maxDimensionKeys {
		counts[value]++
		return
	}
	counts["(other)"]++
}

func mergeCounts(target, source map[string]int) {
	for name, count := range source {
		target[name] += count
	}
}

func topCounts(counts map[string]int, limit int) []namedCount {
	result := make([]namedCount, 0, len(counts))
	for name, count := range counts {
		result = append(result, namedCount{Name: name, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count == result[j].Count {
			return result[i].Name < result[j].Name
		}
		return result[i].Count > result[j].Count
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func newVisitsAuth(username, password string) *visitsAuth {
	return &visitsAuth{
		usernameHash: sha256.Sum256([]byte(username)),
		passwordHash: sha256.Sum256([]byte(password)),
		configured:   username != "" && password != "",
		sessions:     make(map[[sha256.Size]byte]time.Time),
	}
}

func (a *visitsAuth) credentialsMatch(username, password string) bool {
	providedUsernameHash := sha256.Sum256([]byte(username))
	providedPasswordHash := sha256.Sum256([]byte(password))
	usernameMatches := subtle.ConstantTimeCompare(providedUsernameHash[:], a.usernameHash[:])
	passwordMatches := subtle.ConstantTimeCompare(providedPasswordHash[:], a.passwordHash[:])
	return usernameMatches&passwordMatches == 1
}

func (a *visitsAuth) startSession(w http.ResponseWriter, r *http.Request, now time.Time) error {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return err
	}
	expires := now.Add(visitsSessionTTL)
	a.mu.Lock()
	a.purgeExpired(now)
	if len(a.sessions) >= maxVisitsSessions {
		a.evictOldestSession()
	}
	a.sessions[sha256.Sum256(value[:])] = expires
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     visitsCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(value[:]),
		Path:     "/visits",
		Expires:  expires,
		MaxAge:   int(visitsSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   requestUsesHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func (a *visitsAuth) authorized(r *http.Request, now time.Time) bool {
	cookie, err := r.Cookie(visitsCookieName)
	if err != nil {
		return false
	}
	value, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(value) != 32 {
		return false
	}
	key := sha256.Sum256(value)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.purgeExpired(now)
	expires, exists := a.sessions[key]
	return exists && expires.After(now)
}

func (a *visitsAuth) endSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(visitsCookieName); err == nil {
		if value, err := base64.RawURLEncoding.DecodeString(cookie.Value); err == nil && len(value) == 32 {
			a.mu.Lock()
			delete(a.sessions, sha256.Sum256(value))
			a.mu.Unlock()
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     visitsCookieName,
		Path:     "/visits",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestUsesHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (a *visitsAuth) purgeExpired(now time.Time) {
	for key, expires := range a.sessions {
		if !expires.After(now) {
			delete(a.sessions, key)
		}
	}
}

func (a *visitsAuth) evictOldestSession() {
	var oldestKey [sha256.Size]byte
	var oldestExpiry time.Time
	for key, expires := range a.sessions {
		if oldestExpiry.IsZero() || expires.Before(oldestExpiry) {
			oldestKey = key
			oldestExpiry = expires
		}
	}
	if !oldestExpiry.IsZero() {
		delete(a.sessions, oldestKey)
	}
}

func requestUsesHTTPS(r *http.Request) bool {
	forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return r.TLS != nil || strings.EqualFold(forwardedProto, "https")
}

func visitsHandler(store visitsStorage, auth *visitsAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !auth.configured {
			http.Error(w, "visits dashboard is not configured", http.StatusServiceUnavailable)
			return
		}
		if !auth.authorized(r, time.Now()) {
			http.Redirect(w, r, "/visits/login", http.StatusFound)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		snapshot, err := store.Snapshot(time.Now())
		if err != nil {
			logrus.WithError(err).Error("failed to load visits")
			http.Error(w, "failed to load visits", http.StatusInternalServerError)
			return
		}
		var page bytes.Buffer
		if err := visitsPage.Execute(&page, snapshot); err != nil {
			http.Error(w, "failed to render visits dashboard", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(page.Bytes())
	}
}

func visitsLoginHandler(auth *visitsAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.configured {
			http.Error(w, "visits dashboard is not configured", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodGet {
			if auth.authorized(r, time.Now()) {
				http.Redirect(w, r, "/visits", http.StatusFound)
				return
			}
			renderVisitsLogin(w, false, http.StatusOK)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			renderVisitsLogin(w, true, http.StatusBadRequest)
			return
		}
		if !auth.credentialsMatch(r.FormValue("username"), r.FormValue("password")) {
			renderVisitsLogin(w, true, http.StatusUnauthorized)
			return
		}
		if err := auth.startSession(w, r, time.Now()); err != nil {
			logrus.WithError(err).Error("failed to create visits session")
			http.Error(w, "failed to sign in", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/visits", http.StatusSeeOther)
	}
}

func visitsLogoutHandler(auth *visitsAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth.endSession(w, r)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/visits/login", http.StatusSeeOther)
	}
}

func renderVisitsLogin(w http.ResponseWriter, invalid bool, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := visitsLoginPage.Execute(w, struct{ Invalid bool }{Invalid: invalid}); err != nil {
		logrus.WithError(err).Error("failed to render visits login")
	}
}

var visitsLoginPage = template.Must(template.New("visits-login").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex, nofollow">
  <title>Sign in · Visits</title>
  <link rel="stylesheet" href="/visits.css">
</head>
<body class="login-body">
<main class="login-shell">
  <section class="login-card" aria-labelledby="login-heading">
    <p class="eyebrow">Private analytics</p>
    <h1 id="login-heading">Welcome back</h1>
    <p>Sign in to view visits.</p>
    {{if .Invalid}}<p class="login-error" role="alert">The username or password is incorrect.</p>{{end}}
    <form method="post" action="/visits/login" class="login-form">
      <label for="username">Username</label>
      <input id="username" name="username" type="text" autocomplete="username" required autofocus>
      <label for="password">Password</label>
      <input id="password" name="password" type="password" autocomplete="current-password" required>
      <button type="submit">Sign in</button>
    </form>
  </section>
</main>
</body>
</html>`))

var visitsPage = template.Must(template.New("visits").Funcs(template.FuncMap{"bytes": formatBytes}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex, nofollow">
  <title>Visits</title>
  <link rel="stylesheet" href="/visits.css">
</head>
<body>
<main>
  <header class="dashboard-header">
    <div>
      <p class="eyebrow">Private analytics</p>
      <h1>Visits</h1>
      <p>Seven UTC calendar days starting {{.Since.Format "2 January 2006"}}. {{.StorageNote}}</p>
    </div>
    <form method="post" action="/visits/logout"><button class="secondary-button" type="submit">Sign out</button></form>
  </header>

  <section aria-labelledby="overview-heading">
    <h2 id="overview-heading">Overview</h2>
    <div class="metrics">
      <article><strong>{{.Total}}</strong><span>Visits</span></article>
      <article><strong>{{.Unique}}{{if .UniqueIsMinimum}}+{{end}}</strong><span>Unique IPs</span></article>
      <article><strong>{{len .Recent}}</strong><span>Recent visits retained</span></article>
      <article><strong>{{bytes .EstimatedBytes}}</strong><span>{{.StorageLabel}}</span></article>
    </div>
  </section>

  <section aria-labelledby="chart-heading">
    <div class="section-heading"><h2 id="chart-heading">Last 30 days</h2><span>Peak: {{.ChartPeak}} visits/day</span></div>
    <div class="chart-wrap">
      <svg class="visits-chart" viewBox="0 0 900 285" role="img" aria-labelledby="chart-title chart-description">
        <title id="chart-title">Visits during the last 30 days</title>
        <desc id="chart-description">Daily request totals in UTC. Each bar includes its date and exact number of visits.</desc>
        <line x1="24" y1="210" x2="876" y2="210"></line>
        {{range .ChartDays}}
          <rect x="{{.X}}" y="{{.Y}}" width="18" height="{{.Height}}"><title>{{.Date}}: {{.Visits}} visits</title></rect>
          <text class="visit-count" x="{{.CenterX}}" y="{{.ValueY}}">{{.Visits}}</text>
          <text class="date-label" x="{{.CenterX}}" y="224">{{.Label}}</text>
        {{end}}
      </svg>
    </div>
  </section>

  <div class="top-grid">
    <section><h2>Top paths</h2>{{template "ranking" .TopPaths}}</section>
    <section><h2>Top referrers</h2>{{template "ranking" .TopReferrers}}</section>
    <section><h2>Top user agents</h2>{{template "ranking" .TopUserAgents}}</section>
  </div>

  <section aria-labelledby="recent-heading">
    <h2 id="recent-heading">Last 1,000 visits</h2>
    <div class="table-wrap"><table class="visits-table">
      <thead><tr><th scope="col">Time (UTC)</th><th scope="col">Method</th><th scope="col">Path</th><th scope="col">Status</th><th scope="col">Duration</th><th scope="col">Bytes</th><th scope="col">IP</th><th scope="col">Referrer</th><th scope="col">User agent</th></tr></thead>
      <tbody>{{range .Recent}}<tr><td>{{.Time.UTC.Format "2006-01-02 15:04:05"}}</td><td>{{.Method}}</td><td>{{.Path}}</td><td>{{.Status}}</td><td>{{.Duration}}</td><td>{{.ResponseBytes}}</td><td>{{.RemoteIP}}</td><td>{{.Referer}}</td><td>{{.UserAgent}}</td></tr>{{else}}<tr><td colspan="9">No visits recorded yet.</td></tr>{{end}}</tbody>
    </table></div>
  </section>
  <footer>Generated {{.GeneratedAt.Format "2006-01-02 15:04:05 UTC"}}</footer>
</main>
</body>
</html>
{{define "ranking"}}<ol class="ranking">{{range .}}<li><span>{{.Name}}</span><strong>{{.Count}}</strong></li>{{else}}<li>No data yet.</li>{{end}}</ol>{{end}}`))

const visitsCSS = `
:root { color-scheme: light dark; font-family: ui-sans-serif, system-ui, sans-serif; background: #101419; color: #eaf0f6; }
* { box-sizing: border-box; }
body { margin: 0; background: #101419; color: #eaf0f6; }
main { width: min(1440px, 100%); margin: 0 auto; padding: 3rem 1.25rem; }
header { margin-bottom: 2.5rem; }
h1 { margin: 0; font-size: clamp(2.5rem, 8vw, 5rem); letter-spacing: -.05em; }
h2 { margin-top: 0; font-size: 1.15rem; }
.eyebrow { color: #71d4b4; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; }
.dashboard-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 2rem; }
.dashboard-header form { flex: 0 0 auto; }
section { margin-bottom: 2rem; }
.login-body { min-height: 100vh; }
.login-shell { min-height: 100vh; display: grid; place-items: center; padding: 1.5rem; }
.login-card { width: min(28rem, 100%); margin: 0; padding: 2rem; border: 1px solid #303b47; border-radius: 1rem; background: #171d24; box-shadow: 0 1.5rem 4rem rgba(0, 0, 0, .3); }
.login-card h1 { font-size: clamp(2.25rem, 10vw, 3.5rem); }
.login-card > p:not(.eyebrow):not(.login-error) { color: #aab7c4; }
.login-form { display: grid; gap: .65rem; margin-top: 1.75rem; }
.login-form label { margin-top: .5rem; font-weight: 700; }
.login-form input { width: 100%; padding: .85rem 1rem; border: 1px solid #526170; border-radius: .55rem; background: #101419; color: #eaf0f6; font: inherit; }
.login-form input:focus { outline: 3px solid rgba(113, 212, 180, .3); border-color: #71d4b4; }
button { padding: .8rem 1rem; border: 1px solid #71d4b4; border-radius: .55rem; background: #71d4b4; color: #101419; font: inherit; font-weight: 800; cursor: pointer; }
button:hover { background: #a4ead3; }
button:focus-visible { outline: 3px solid rgba(113, 212, 180, .4); outline-offset: 2px; }
.login-form button { margin-top: 1rem; }
.secondary-button { background: transparent; color: #eaf0f6; }
.secondary-button:hover { background: #243039; }
.login-error { padding: .8rem 1rem; border: 1px solid #ef8d8d; border-radius: .55rem; background: rgba(176, 47, 47, .18); color: #ffd4d4; }
.metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 1rem; }
.top-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 1rem; }
.metrics article, .top-grid section { padding: 1.25rem; border: 1px solid #303b47; border-radius: .75rem; background: #171d24; }
.metrics strong { display: block; font-size: 2.25rem; color: #71d4b4; }
.metrics span { color: #aab7c4; }
.memory-note { color: #aab7c4; font-size: .85rem; }
.section-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 1rem; }
.section-heading span { color: #aab7c4; font-size: .85rem; }
.chart-wrap { overflow-x: auto; padding: 1rem; border: 1px solid #303b47; border-radius: .75rem; background: #171d24; }
.visits-chart { display: block; width: 100%; min-width: 700px; height: auto; }
.visits-chart line { stroke: #526170; stroke-width: 1; }
.visits-chart rect { fill: #71d4b4; }
.visits-chart rect:hover { fill: #a4ead3; }
.visits-chart text { fill: #aab7c4; font-size: 9px; text-anchor: middle; }
.visits-chart .visit-count { fill: #eaf0f6; font-size: 10px; font-weight: 700; }
.visits-chart .date-label { text-anchor: start; writing-mode: vertical-rl; }
.table-wrap { overflow-x: auto; border: 1px solid #303b47; border-radius: .75rem; }
table { width: 100%; border-collapse: collapse; background: #171d24; }
th, td { padding: .75rem; border-bottom: 1px solid #303b47; text-align: left; vertical-align: top; }
th { color: #aab7c4; font-size: .75rem; letter-spacing: .06em; text-transform: uppercase; }
tbody tr:last-child td { border-bottom: 0; }
.visits-table { min-width: 1100px; font-size: .85rem; }
.visits-table td:nth-child(3), .visits-table td:nth-child(8), .visits-table td:nth-child(9) { max-width: 24rem; overflow-wrap: anywhere; }
.ranking { margin: 0; padding: 0; list-style: none; }
.ranking li { display: flex; justify-content: space-between; gap: 1rem; padding: .55rem 0; border-bottom: 1px solid #303b47; overflow-wrap: anywhere; }
.ranking li:last-child { border-bottom: 0; }
.ranking strong { color: #71d4b4; }
footer { color: #7f8e9d; font-size: .85rem; }
@media (max-width: 900px) { .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 600px) { main { padding-top: 2rem; } .metrics, .top-grid { grid-template-columns: 1fr; } .dashboard-header { display: block; } }
`
