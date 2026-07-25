package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGebiedenHTTPGet_Success covers the happy path: a 200 response is returned as a readable
// body, and any caller-supplied headers (e.g. Accept-Crs for RD paging) are sent on the wire.
func TestGebiedenHTTPGet_Success(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Accept-Crs")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	body, err := gebiedenHTTPGet(context.Background(), srv.URL, map[string]string{"Accept-Crs": "EPSG:28992"})
	require.NoError(t, err)
	require.NotNil(t, body)
	defer func() { _ = body.Close() }()

	got, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, string(got))
	assert.Equal(t, "EPSG:28992", gotHeader, "the header map must be forwarded onto the request")
}

// TestGebiedenHTTPGet_NonOK covers the rejection path: a non-200 response must be surfaced as
// an error naming the status, with no body handed back to the caller to leak.
func TestGebiedenHTTPGet_NonOK(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "500 internal server error", status: http.StatusInternalServerError},
		{name: "404 not found", status: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("boom"))
			}))
			defer srv.Close()

			body, err := gebiedenHTTPGet(context.Background(), srv.URL, nil)
			require.Error(t, err)
			assert.Nil(t, body, "no body should be returned for a rejected response")
			assert.Contains(t, err.Error(), strconv.Itoa(tt.status), "error should mention the status code")
		})
	}
}
