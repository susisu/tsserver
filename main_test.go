package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/_tsserver/typeeval"
)

func newRouterForApp(t *testing.T, appDir string) http.Handler {
	t.Helper()
	evaluator, err := typeeval.Load(context.Background(), appDir)
	if err != nil {
		t.Fatalf("load app %q: %v", appDir, err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newRouter(logger, evaluator)
}

func doRequest(t *testing.T, router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestServeApp(t *testing.T) {
	router := newRouterForApp(t, "./testdata/app")

	rec := doRequest(t, router, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if want := "ok\n"; rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if n, err := strconv.Atoi(rec.Header().Get(instantiationsHeader)); err != nil || n <= 0 {
		t.Errorf("%s = %q, want a positive integer", instantiationsHeader, rec.Header().Get(instantiationsHeader))
	}

	rec = doRequest(t, router, httptest.NewRequest("GET", "/nowhere", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	rec = doRequest(t, router, httptest.NewRequest("POST", "/", strings.NewReader("x")))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestRequestBodyTooLarge(t *testing.T) {
	router := newRouterForApp(t, "./testdata/app")
	body := strings.Repeat("x", maxRequestBodyBytes+1)
	rec := doRequest(t, router, httptest.NewRequest("POST", "/", strings.NewReader(body)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}
