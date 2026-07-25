package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
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
			assert.Equal(t, tt.want, configFromEnv(env))
		})
	}
}

// TestHealthHandler confirms /healthz returns 200 with the expected body.
func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	newHandler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body, _ := io.ReadAll(rec.Result().Body)
	assert.Equal(t, "ok\n", string(body))
}

// TestRunHealthcheck drives the -healthcheck probe against a live test server
// (healthy) and against a closed port (unhealthy).
func TestRunHealthcheck(t *testing.T) {
	srv := httptest.NewServer(newHandler())
	defer srv.Close()

	// srv.Listener.Addr() is a concrete host:port the probe can hit.
	assert.NoError(t, runHealthcheck(srv.Listener.Addr().String()),
		"healthcheck against live server")

	// A closed listener: the same address after Close should error.
	closed := httptest.NewServer(newHandler())
	addr := closed.Listener.Addr().String()
	closed.Close()
	assert.Error(t, runHealthcheck(addr), "healthcheck against closed server")
}
