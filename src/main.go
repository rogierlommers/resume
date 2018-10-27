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

		referer := r.Header.Get("Referer")
		userAgent := r.Header.Get("User-Agent")
		remoteAddr := r.RemoteAddr

		logrus.WithFields(logrus.Fields{
			"referer":     referer,
			"user_agent":  userAgent,
			"remote_addr": remoteAddr,
		}).Infof("incoming request: %s", r.RequestURI)

		next.ServeHTTP(w, r)
	})
}
