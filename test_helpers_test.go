package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newFakeJira(t *testing.T, handler http.HandlerFunc) (*httptest.Server, Settings) {
	t.Helper()
	server := httptest.NewServer(handler)
	settings := defaultSettings()
	settings.Site = server.URL
	settings.Email = "user@example.com"
	settings.Token = "secret-token"
	settings.Project = "ABC"
	settings.Workdir = t.TempDir()
	settings.normalize()
	return server, settings
}

func requireBasicAuth(t *testing.T, r *http.Request) {
	t.Helper()
	user, password, ok := r.BasicAuth()
	if !ok {
		t.Errorf("%s %s missing basic auth", r.Method, r.URL.Path)
		return
	}
	if user != "user@example.com" || password != "secret-token" {
		t.Errorf("unexpected credentials %q/%q", user, password)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func readTestBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("read body: %v", err)
	}
	return data
}
