package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestIsValidPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path  string
		valid bool
	}{
		{path: "/", valid: true},
		{path: "/Resume%20-%20Rogier%20Lommers.pdf", valid: true},
		{path: "/nested/file.txt", valid: true},
		{path: "/../secret.txt", valid: false},
		{path: "/safe/../../secret.txt", valid: false},
		{path: "/\x00", valid: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			if got := isValidPath(tt.path); got != tt.valid {
				t.Fatalf("isValidPath(%q) = %v, want %v", tt.path, got, tt.valid)
			}
		})
	}
}

func TestNewRouterServesAssets(t *testing.T) {
	t.Parallel()

	handler := newRouter(http.FS(fstest.MapFS{
		"index.html": {Data: []byte("hello")},
		"resume.pdf": {Data: []byte("pdf")},
	}))

	tests := []struct {
		name string
		path string
		want int
		body string
	}{
		{name: "index", path: "/", want: http.StatusOK, body: "hello"},
		{name: "pdf", path: "/resume.pdf", want: http.StatusOK, body: "pdf"},
		{name: "missing", path: "/missing.txt", want: http.StatusNotFound},
		{name: "health", path: "/healthz", want: http.StatusOK, body: "ok\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if tt.body != "" {
				body, err := io.ReadAll(rec.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if string(body) != tt.body {
					t.Fatalf("body = %q, want %q", body, tt.body)
				}
			}
		})
	}
}
