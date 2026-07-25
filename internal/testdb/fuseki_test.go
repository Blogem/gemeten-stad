//go:build integration

package testdb

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestFusekiDatasetLifecycle(t *testing.T) {
	baseURL := requireEnv(t, "GS_TEST_FUSEKI_URL")
	adminPassword := requireEnv(t, "FUSEKI_ADMIN_PASSWORD")
	ctx := context.Background()

	datasetName, err := NewDatasetName()
	if err != nil {
		t.Fatalf("NewDatasetName: %v", err)
	}

	if err := CreateDataset(ctx, baseURL, adminPassword, datasetName); err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	t.Cleanup(func() {
		if err := DropDataset(ctx, baseURL, adminPassword, datasetName); err != nil {
			t.Errorf("DropDataset cleanup: %v", err)
		}
	})

	datasetURL := strings.TrimRight(baseURL, "/") + "/" + datasetName

	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, datasetURL+"/data?default",
		strings.NewReader("<urn:testdb:s> <urn:testdb:p> <urn:testdb:o> ."))
	if err != nil {
		t.Fatalf("build PUT request: %v", err)
	}
	putReq.SetBasicAuth(fusekiAdminUser, adminPassword)
	putReq.Header.Set("Content-Type", "text/turtle")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatalf("PUT triple: %v", err)
	}
	defer func() { _ = putResp.Body.Close() }()
	if putResp.StatusCode/100 != 2 {
		t.Fatalf("PUT triple: status %d", putResp.StatusCode)
	}

	askReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		datasetURL+"/sparql?query="+url.QueryEscape("ASK { <urn:testdb:s> <urn:testdb:p> <urn:testdb:o> }"), nil)
	if err != nil {
		t.Fatalf("build ASK request: %v", err)
	}
	askReq.SetBasicAuth(fusekiAdminUser, adminPassword)
	askReq.Header.Set("Accept", "application/sparql-results+json")
	askResp, err := http.DefaultClient.Do(askReq)
	if err != nil {
		t.Fatalf("ASK query: %v", err)
	}
	defer func() { _ = askResp.Body.Close() }()
	body, _ := io.ReadAll(askResp.Body)
	if askResp.StatusCode/100 != 2 {
		t.Fatalf("ASK query: status %d: %s", askResp.StatusCode, body)
	}
	if !strings.Contains(string(body), "true") {
		t.Fatalf("expected the written triple to be found in the isolated dataset, got %s", body)
	}
}

func TestFusekiGuardAbortsOnProductionName(t *testing.T) {
	baseURL := requireEnv(t, "GS_TEST_FUSEKI_URL")
	adminPassword := requireEnv(t, "FUSEKI_ADMIN_PASSWORD")
	ctx := context.Background()

	if err := CreateDataset(ctx, baseURL, adminPassword, "ds"); err == nil {
		t.Fatal("CreateDataset against the production dataset name must abort, got nil error")
	}
}
