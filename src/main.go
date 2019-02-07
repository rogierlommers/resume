package main

import (
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

func main() {
	router := mux.NewRouter()
	router.Use(loggingMiddleware)
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("assets")))

	cwd, _ := os.Getwd()

	logrus.Infof("serving on :8080, cwd: %s", cwd)
	if err := http.ListenAndServe(":8080", router); err != nil {
		logrus.Fatal(err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		logrus.WithFields(logrus.Fields{
			"requestURI":      r.RequestURI,
			"X-Forwarded-For": r.Header.Get("X-Forwarded-For"),
		}).Info("incoming request")

		next.ServeHTTP(w, r)
	})
}
