package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

func main() {
	router := newRouter(http.Dir("assets"))

	cwd, _ := os.Getwd()

	logrus.Infof("serving on http://localhost:8080, cwd: %s", cwd)
	if err := http.ListenAndServe(":8080", router); err != nil {
		logrus.WithFields(logrus.Fields{
			"address": ":8080",
			"router":  router,
		}).Fatal("Failed to start server: ", err)
	}
}

func newRouter(assets http.FileSystem) http.Handler {
	router := mux.NewRouter()
	router.Use(loggingMiddleware)

	fileServer := http.FileServer(assets)
	router.PathPrefix("/").Handler(http.StripPrefix("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isValidPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})))

	return router
}

func isValidPath(path string) bool {
	if strings.Contains(path, "\x00") {
		return false
	}

	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return false
		}
	}

	return true
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
