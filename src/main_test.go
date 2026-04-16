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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if string(body) != "hello" {
		t.Fatalf("body = %q, want %q", body, "hello")
	}
}
