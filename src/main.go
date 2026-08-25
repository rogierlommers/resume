package main

import (
	"context"
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
	router := newRouter(http.Dir("assets"))

	address := os.Getenv("ADDRESS")
	if address == "" {
		address = ":8080"
	}
	logrus.WithField("access_logs", accessLogs).Info("access logs configured")

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
	router := http.NewServeMux()
	router.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	fileServer := http.FileServer(assets)
	router.Handle("/", http.StripPrefix("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isValidPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})))

	return responseMiddleware(router, accessLogsEnabled(os.Getenv("ACCESS_LOGS")))
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

func responseMiddleware(next http.Handler, accessLogs bool) http.Handler {
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
		if accessLogs {
			logrus.WithFields(logrus.Fields{
				"method":   r.Method,
				"path":     r.URL.Path,
				"status":   status,
				"duration": time.Since(started).String(),
			}).Info("request completed")
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
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
	return w.ResponseWriter.Write(body)
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
