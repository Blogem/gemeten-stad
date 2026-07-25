package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestConfigFromEnv checks the env-driven config: dev defaults when unset, and
// overrides when the GS_* vars are present.
func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want config
	}{
		{
			name: "defaults",
			env:  map[string]string{},
			want: config{addr: ":8080", fusekiURL: "", databaseURL: ""},
		},
		{
			name: "overrides",
			env: map[string]string{
				"GS_SERVER_ADDR":  ":9090",
				"GS_FUSEKI_URL":   "http://fuseki:3030/ds",
				"GS_DATABASE_URL": "postgres://gs@db:5432/gemeten_stad",
			},
			want: config{
				addr:        ":9090",
				fusekiURL:   "http://fuseki:3030/ds",
				databaseURL: "postgres://gs@db:5432/gemeten_stad",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := func(k string) string { return tt.env[k] }
			if got := configFromEnv(env); got != tt.want {
				t.Errorf("configFromEnv() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestHealthHandler confirms /healthz returns 200 with the expected body.
func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	newHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Result().Body)
	if got, want := string(body), "ok\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// TestRunHealthcheck drives the -healthcheck probe against a live test server
// (healthy) and against a closed port (unhealthy).
func TestRunHealthcheck(t *testing.T) {
	srv := httptest.NewServer(newHandler())
	defer srv.Close()

	// srv.Listener.Addr() is a concrete host:port the probe can hit.
	if err := runHealthcheck(srv.Listener.Addr().String()); err != nil {
		t.Errorf("healthcheck against live server failed: %v", err)
	}

	// A closed listener: the same address after Close should error.
	closed := httptest.NewServer(newHandler())
	addr := closed.Listener.Addr().String()
	closed.Close()
	if err := runHealthcheck(addr); err == nil {
		t.Error("healthcheck against closed server: got nil error, want failure")
	}
}
