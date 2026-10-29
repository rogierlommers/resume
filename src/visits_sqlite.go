package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteVisitStore struct {
	db *sql.DB
}

func newSQLiteVisitStore(path string) (*sqliteVisitStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open visits database: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &sqliteVisitStore{db: db}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *sqliteVisitStore) initialize() error {
	pragmas := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
	}
	for _, statement := range pragmas {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("configure visits database: %w", err)
		}
	}
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read visits database schema version: %w", err)
	}
	if version > 1 {
		return fmt.Errorf("visits database schema version %d is newer than supported version 1", version)
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS visits (
			id INTEGER PRIMARY KEY,
			visited_at_ns INTEGER NOT NULL,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			status INTEGER NOT NULL,
			duration_ns INTEGER NOT NULL,
			response_bytes INTEGER NOT NULL,
			remote_ip TEXT NOT NULL,
			host TEXT NOT NULL,
			protocol TEXT NOT NULL,
			user_agent TEXT NOT NULL,
			referer TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS visits_visited_at_idx ON visits (visited_at_ns)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("initialize visits database: %w", err)
		}
	}
	if _, err := s.db.Exec(`PRAGMA user_version = 1`); err != nil {
		return fmt.Errorf("set visits database schema version: %w", err)
	}
	return nil
}

func (s *sqliteVisitStore) Close() error {
	return s.db.Close()
}

func (s *sqliteVisitStore) Record(v visit) error {
	v = normalizeVisit(v)
	cutoff := startOfUTCDay(v.Time).AddDate(0, 0, -(aggregateDays - 1)).UnixNano()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin visit transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM visits WHERE visited_at_ns < ?`, cutoff); err != nil {
		return fmt.Errorf("purge visits: %w", err)
	}
	_, err = tx.Exec(`INSERT INTO visits (
		visited_at_ns, method, path, status, duration_ns, response_bytes,
		remote_ip, host, protocol, user_agent, referer
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Time.UTC().UnixNano(), v.Method, v.Path, v.Status, int64(v.Duration), v.ResponseBytes,
		v.RemoteIP, v.Host, v.Protocol, v.UserAgent, v.Referer,
	)
	if err != nil {
		return fmt.Errorf("insert visit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit visit: %w", err)
	}
	return nil
}

func (s *sqliteVisitStore) Snapshot(now time.Time) (visitSnapshot, error) {
	now = now.UTC()
	chartSince := startOfUTCDay(now).AddDate(0, 0, -(aggregateDays - 1))
	if _, err := s.db.Exec(`DELETE FROM visits WHERE visited_at_ns < ?`, chartSince.UnixNano()); err != nil {
		return visitSnapshot{}, fmt.Errorf("purge visits: %w", err)
	}

	since := startOfUTCDay(now).AddDate(0, 0, -6)
	until := startOfUTCDay(now).AddDate(0, 0, 1)
	snapshot := visitSnapshot{
		GeneratedAt:  now,
		Since:        since,
		StorageLabel: "SQLite database size",
		StorageNote:  "Data persists in SQLite for 30 UTC calendar days.",
	}
	if err := s.db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT NULLIF(remote_ip, ''))
		FROM visits WHERE visited_at_ns >= ? AND visited_at_ns < ?`,
		since.UnixNano(), until.UnixNano()).Scan(&snapshot.Total, &snapshot.Unique); err != nil {
		return visitSnapshot{}, fmt.Errorf("summarize visits: %w", err)
	}

	var err error
	if snapshot.TopPaths, err = s.topCounts("path", "(no path)", since, until); err != nil {
		return visitSnapshot{}, err
	}
	if snapshot.TopReferrers, err = s.topCounts("referer", "(direct)", since, until); err != nil {
		return visitSnapshot{}, err
	}
	if snapshot.TopUserAgents, err = s.topCounts("user_agent", "(unknown)", since, until); err != nil {
		return visitSnapshot{}, err
	}
	if snapshot.Recent, err = s.recentVisits(); err != nil {
		return visitSnapshot{}, err
	}
	if snapshot.ChartDays, snapshot.ChartPeak, err = s.sqliteChartDays(now, chartSince, until); err != nil {
		return visitSnapshot{}, err
	}
	if snapshot.EstimatedBytes, err = s.databaseBytes(); err != nil {
		return visitSnapshot{}, err
	}
	return snapshot, nil
}

func (s *sqliteVisitStore) topCounts(column, fallback string, since, until time.Time) ([]namedCount, error) {
	// column is selected exclusively from constants at the call sites above.
	query := fmt.Sprintf(`SELECT COALESCE(NULLIF(%s, ''), ?), COUNT(*) FROM visits
		WHERE visited_at_ns >= ? AND visited_at_ns < ?
		GROUP BY COALESCE(NULLIF(%s, ''), ?)
		ORDER BY COUNT(*) DESC, COALESCE(NULLIF(%s, ''), ?) ASC LIMIT 10`, column, column, column)
	rows, err := s.db.Query(query, fallback, since.UnixNano(), until.UnixNano(), fallback, fallback)
	if err != nil {
		return nil, fmt.Errorf("query top %s: %w", column, err)
	}
	defer rows.Close()
	var result []namedCount
	for rows.Next() {
		var count namedCount
		if err := rows.Scan(&count.Name, &count.Count); err != nil {
			return nil, fmt.Errorf("scan top %s: %w", column, err)
		}
		result = append(result, count)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top %s: %w", column, err)
	}
	return result, nil
}

func (s *sqliteVisitStore) recentVisits() ([]visit, error) {
	rows, err := s.db.Query(`SELECT visited_at_ns, method, path, status, duration_ns,
		response_bytes, remote_ip, host, protocol, user_agent, referer
		FROM visits ORDER BY visited_at_ns DESC, id DESC LIMIT ?`, maxRecentVisits)
	if err != nil {
		return nil, fmt.Errorf("query recent visits: %w", err)
	}
	defer rows.Close()
	var result []visit
	for rows.Next() {
		var v visit
		var visitedAt, duration int64
		if err := rows.Scan(&visitedAt, &v.Method, &v.Path, &v.Status, &duration,
			&v.ResponseBytes, &v.RemoteIP, &v.Host, &v.Protocol, &v.UserAgent, &v.Referer); err != nil {
			return nil, fmt.Errorf("scan recent visit: %w", err)
		}
		v.Time = time.Unix(0, visitedAt).UTC()
		v.Duration = time.Duration(duration)
		result = append(result, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent visits: %w", err)
	}
	return result, nil
}

func (s *sqliteVisitStore) sqliteChartDays(now, since, until time.Time) ([]chartDay, int, error) {
	rows, err := s.db.Query(`SELECT strftime('%Y-%m-%d', visited_at_ns / 1000000000, 'unixepoch'), COUNT(*)
		FROM visits WHERE visited_at_ns >= ? AND visited_at_ns < ? GROUP BY 1`, since.UnixNano(), until.UnixNano())
	if err != nil {
		return nil, 0, fmt.Errorf("query visit chart: %w", err)
	}
	counts := make(map[string]int)
	for rows.Next() {
		var date string
		var count int
		if err := rows.Scan(&date, &count); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("scan visit chart: %w", err)
		}
		counts[date] = count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("iterate visit chart: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, fmt.Errorf("close visit chart: %w", err)
	}

	memory := newVisitStore()
	for date, count := range counts {
		memory.days[date] = &visitDay{total: count}
	}
	days, peak := memory.chartDays(now)
	return days, peak, nil
}

func (s *sqliteVisitStore) databaseBytes() (uint64, error) {
	var pageCount, pageSize uint64
	if err := s.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil {
		return 0, fmt.Errorf("query database page count: %w", err)
	}
	if err := s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0, fmt.Errorf("query database page size: %w", err)
	}
	return pageCount * pageSize, nil
}
