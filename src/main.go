package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

func main() {
	accessLogs := accessLogsEnabled(os.Getenv("ACCESS_LOGS"))
	visitsToken := os.Getenv("VISITS_TOKEN")
	router := newRouterWithConfig(http.Dir("assets"), accessLogs, visitsToken, newVisitStore())

	address := os.Getenv("ADDRESS")
	if address == "" {
		address = ":8080"
	}
	logrus.WithFields(logrus.Fields{
		"access_logs":      accessLogs,
		"visits_dashboard": visitsToken != "",
	}).Info("server features configured")

	server := &http.Server{
		Addr:              address,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logrus.Infof("serving on http://localhost%s", address)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logrus.WithError(err).Error("failed to shut down server gracefully")
		}
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logrus.WithFields(logrus.Fields{
			"address": address,
		}).WithError(err).Fatal("failed to start server")
	}
}

func newRouter(assets http.FileSystem) http.Handler {
	return newRouterWithConfig(assets, accessLogsEnabled(os.Getenv("ACCESS_LOGS")), os.Getenv("VISITS_TOKEN"), newVisitStore())
}

func newRouterWithConfig(assets http.FileSystem, accessLogs bool, visitsToken string, visits *visitStore) http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	router.HandleFunc("GET /visits", visitsHandler(visits, visitsToken))
	router.HandleFunc("GET /visits.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(visitsCSS))
	})

	fileServer := http.FileServer(assets)
	router.Handle("/", http.StripPrefix("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isValidPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})))

	return responseMiddleware(router, accessLogs, visits)
}

func accessLogsEnabled(value string) bool {
	return value == "true"
}

func isValidPath(path string) bool {
	if strings.Contains(path, "\x00") {
		return false
	}

	return !slices.Contains(strings.Split(path, "/"), "..")
}

func responseMiddleware(next http.Handler, accessLogs bool, visits *visitStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		writer := &statusWriter{ResponseWriter: w}
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' https://fonts.googleapis.com 'sha256-s+iOt/vS3ez0Yz+WtHaup4LAL9/ttRjC6Q+1zgzmiQg='; font-src https://fonts.gstatic.com; img-src 'self'; frame-ancestors 'none'")
		writer.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		writer.Header().Set("X-Content-Type-Options", "nosniff")

		next.ServeHTTP(writer, r)

		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(started)
		if visits != nil && shouldRecordVisit(r.URL.Path) {
			visits.record(visit{
				Time:          started,
				Method:        r.Method,
				Path:          r.URL.Path,
				Status:        status,
				Duration:      duration,
				ResponseBytes: writer.bytes,
				RemoteIP:      remoteIP(r.RemoteAddr),
				Host:          r.Host,
				Protocol:      r.Proto,
				UserAgent:     r.UserAgent(),
				Referer:       r.Referer(),
			})
		}
		if accessLogs {
			logrus.WithFields(logrus.Fields{
				"method":         r.Method,
				"path":           r.URL.Path,
				"status":         status,
				"duration":       duration.String(),
				"duration_ms":    float64(duration.Microseconds()) / 1000,
				"response_bytes": writer.bytes,
				"remote_ip":      remoteIP(r.RemoteAddr),
				"host":           r.Host,
				"protocol":       r.Proto,
				"user_agent":     r.UserAgent(),
				"referer":        sanitizeReferer(r.Referer()),
			}).Info("request completed")
		}
	})
}

func shouldRecordVisit(path string) bool {
	return path != "/visits" && path != "/visits.css"
}

func remoteIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
