package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

func main() {
	router := mux.NewRouter()
	router.Use(loggingMiddleware)
	router.PathPrefix("/").Handler(http.StripPrefix("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isValidPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		http.FileServer(http.Dir("assets")).ServeHTTP(w, r)
	})))

	cwd, _ := os.Getwd()

	logrus.Infof("serving on http://localhost:8080, cwd: %s", cwd)
	if err := http.ListenAndServe(":8080", router); err != nil {
		logrus.WithFields(logrus.Fields{
			"address": ":8080",
			"router":  router,
		}).Fatal("Failed to start server: ", err)
	}
}

func isValidPath(path string) bool {
	// Add security checks to ensure the path is within the "assets" directory
	// For example, you can check for ".." to prevent directory traversal attacks
	return !strings.Contains(path, "..")
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// logrus.WithFields(logrus.Fields{
		// 	"requestURI":      r.RequestURI,
		// 	"X-Forwarded-For": r.Header.Get("X-Forwarded-For"),
		// }).Info("incoming request")

		next.ServeHTTP(w, r)
	})
}
