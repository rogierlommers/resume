package main

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

func main() {
	router := mux.NewRouter()
	router.Use(loggingMiddleware)
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("assets")))

	logrus.Info("serving on :8080")
	if err := http.ListenAndServe(":8080", router); err != nil {
		logrus.Fatal(err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logrus.Infof("incoming request: %s", r.RequestURI)
		next.ServeHTTP(w, r)
	})
}
