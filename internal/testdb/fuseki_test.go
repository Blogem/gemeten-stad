//go:build integration

package testdb

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFusekiDatasetLifecycle(t *testing.T) {
	baseURL := requireEnv(t, "GS_TEST_FUSEKI_URL")
	adminPassword := requireEnv(t, "FUSEKI_ADMIN_PASSWORD")
	ctx := context.Background()

	datasetName, err := NewDatasetName()
	require.NoError(t, err)

	require.NoError(t, CreateDataset(ctx, baseURL, adminPassword, datasetName))
	t.Cleanup(func() {
		require.NoError(t, DropDataset(ctx, baseURL, adminPassword, datasetName), "DropDataset cleanup")
	})

	datasetURL := strings.TrimRight(baseURL, "/") + "/" + datasetName

	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, datasetURL+"/data?default",
		strings.NewReader("<urn:testdb:s> <urn:testdb:p> <urn:testdb:o> ."))
	require.NoError(t, err, "build PUT request")
	putReq.SetBasicAuth(fusekiAdminUser, adminPassword)
	putReq.Header.Set("Content-Type", "text/turtle")
	putResp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err, "PUT triple")
	defer func() { _ = putResp.Body.Close() }()
	require.Equalf(t, 2, putResp.StatusCode/100, "PUT triple: status %d", putResp.StatusCode)

	askReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		datasetURL+"/sparql?query="+url.QueryEscape("ASK { <urn:testdb:s> <urn:testdb:p> <urn:testdb:o> }"), nil)
	require.NoError(t, err, "build ASK request")
	askReq.SetBasicAuth(fusekiAdminUser, adminPassword)
	askReq.Header.Set("Accept", "application/sparql-results+json")
	askResp, err := http.DefaultClient.Do(askReq)
	require.NoError(t, err, "ASK query")
	defer func() { _ = askResp.Body.Close() }()
	body, _ := io.ReadAll(askResp.Body)
	require.Equalf(t, 2, askResp.StatusCode/100, "ASK query: status %d: %s", askResp.StatusCode, body)
	require.Containsf(t, string(body), "true",
		"expected the written triple to be found in the isolated dataset, got %s", body)
}

func TestFusekiGuardAbortsOnProductionName(t *testing.T) {
	baseURL := requireEnv(t, "GS_TEST_FUSEKI_URL")
	adminPassword := requireEnv(t, "FUSEKI_ADMIN_PASSWORD")
	ctx := context.Background()

	require.Error(t, CreateDataset(ctx, baseURL, adminPassword, "ds"),
		"CreateDataset against the production dataset name must abort")
}
