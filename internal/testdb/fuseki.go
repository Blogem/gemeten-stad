package testdb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// fusekiAdminUser is the fixed admin username Fuseki's admin API expects; the
// dev-compose stack (deploy/compose/compose.yaml) only configures the
// password via FUSEKI_ADMIN_PASSWORD.
const fusekiAdminUser = "admin"

// CreateDataset provisions an in-memory TDB2 dataset named datasetName on the
// Fuseki server at baseURL (e.g. "http://localhost:3030") via its admin API.
func CreateDataset(ctx context.Context, baseURL, adminPassword, datasetName string) error {
	if err := AssertNotProduction("", datasetName); err != nil {
		return err
	}

	form := url.Values{"dbName": {datasetName}, "dbType": {"mem"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/$/datasets", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("testdb: build create-dataset request: %w", err)
	}
	req.SetBasicAuth(fusekiAdminUser, adminPassword)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return doAdminRequest(req, "create dataset "+datasetName)
}

// DropDataset removes datasetName from the Fuseki server at baseURL.
func DropDataset(ctx context.Context, baseURL, adminPassword, datasetName string) error {
	if err := AssertNotProduction("", datasetName); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(baseURL, "/")+"/$/datasets/"+datasetName, nil)
	if err != nil {
		return fmt.Errorf("testdb: build drop-dataset request: %w", err)
	}
	req.SetBasicAuth(fusekiAdminUser, adminPassword)

	return doAdminRequest(req, "drop dataset "+datasetName)
}

func doAdminRequest(req *http.Request, action string) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("testdb: %s: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("testdb: %s: status %d: %s", action, resp.StatusCode, body)
	}
	return nil
}
