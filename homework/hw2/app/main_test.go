package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func responseMessage(t *testing.T, handler http.Handler) (string, bool) {
	t.Helper()
	response := &recordingResponseWriter{header: make(http.Header)}
	request, err := http.NewRequest(http.MethodGet, "/message", nil)
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(response, request)
	if response.status != http.StatusOK {
		t.Fatalf("GET /message status = %d, want 200", response.status)
	}
	var body struct {
		Message     string `json:"message"`
		NewGreeting bool   `json:"new_greeting"`
	}
	if err := json.NewDecoder(strings.NewReader(response.body)).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Message, body.NewGreeting
}

type recordingResponseWriter struct {
	header http.Header
	body   string
	status int
}

func (w *recordingResponseWriter) Header() http.Header    { return w.header }
func (w *recordingResponseWriter) WriteHeader(status int) { w.status = status }
func (w *recordingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body += string(data)
	return len(data), nil
}

func TestFeatureFlagRuntimeToggle(t *testing.T) {
	flag := &featureFlag{}
	public := publicMux(flag)
	admin := adminMux(flag)

	message, enabled := responseMessage(t, public)
	if message != oldGreeting || enabled {
		t.Fatalf("initial state = (%q, %v), want (%q, false)", message, enabled, oldGreeting)
	}

	request, err := http.NewRequest(http.MethodGet, "/admin/feature?enabled=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := &recordingResponseWriter{header: make(http.Header)}
	admin.ServeHTTP(response, request)
	if response.status != http.StatusOK {
		t.Fatalf("enable status = %d, want 200", response.status)
	}
	message, enabled = responseMessage(t, public)
	if message != newGreeting || !enabled {
		t.Fatalf("enabled state = (%q, %v), want (%q, true)", message, enabled, newGreeting)
	}

	request, err = http.NewRequest(http.MethodGet, "/admin/feature?enabled=false", nil)
	if err != nil {
		t.Fatal(err)
	}
	response = &recordingResponseWriter{header: make(http.Header)}
	admin.ServeHTTP(response, request)
	if response.status != http.StatusOK {
		t.Fatalf("disable status = %d, want 200", response.status)
	}
	message, enabled = responseMessage(t, public)
	if message != oldGreeting || enabled {
		t.Fatalf("disabled state = (%q, %v), want (%q, false)", message, enabled, oldGreeting)
	}
}

func TestFeatureFlagRejectsInvalidValue(t *testing.T) {
	flag := &featureFlag{}
	request, err := http.NewRequest(http.MethodGet, "/admin/feature?enabled=yes", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := &recordingResponseWriter{header: make(http.Header)}
	adminMux(flag).ServeHTTP(response, request)
	if response.status != http.StatusBadRequest {
		t.Fatalf("invalid value status = %d, want 400", response.status)
	}
}
