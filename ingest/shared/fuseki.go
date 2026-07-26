package shared

import "fmt"

// FusekiURL resolves GS_FUSEKI_URL from env; error (fail loud) if unset/empty.
// It is the Fuseki dataset URL (e.g. "http://fuseki:3030/ds") the graph load path writes to,
// the triplestore analogue of DatabaseURL.
func FusekiURL(env func(string) string) (string, error) {
	url := env("GS_FUSEKI_URL")
	if url == "" {
		return "", fmt.Errorf("shared: GS_FUSEKI_URL is not set")
	}
	return url, nil
}
