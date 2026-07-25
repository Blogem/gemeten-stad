package gebieden

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// fakeGebiedenPage is a minimal Datapunt-shaped page: an empty embedded collection is enough for
// pageAll/BuildFeatureCollection to succeed (a single short page stops paging).
const fakeBuurtenPage = `{"_embedded":{"buurten":[]}}`
const fakeWijkenPage = `{"_embedded":{"wijken":[]}}`

// TestIngest_CBSFailureIsBestEffort asserts D-decision: a CBS WFS failure is logged and
// non-fatal, while the gebieden buurten/wijken artifacts (the primary point-in-polygon set)
// still land.
func TestIngest_CBSFailureIsBestEffort(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())
	cbsURL := CBSWFSURL()
	var calls []string

	fakeGet := func(_ context.Context, url string, _ map[string]string) (io.ReadCloser, error) {
		calls = append(calls, url)
		switch {
		case url == cbsURL:
			return nil, errors.New("cbs wfs unavailable")
		case strings.Contains(url, "gebieden/buurten"):
			return io.NopCloser(strings.NewReader(fakeBuurtenPage)), nil
		case strings.Contains(url, "gebieden/wijken"):
			return io.NopCloser(strings.NewReader(fakeWijkenPage)), nil
		default:
			return nil, fmt.Errorf("unexpected URL %q", url)
		}
	}

	err := Ingest(context.Background(), store, fakeGet)
	require.NoError(t, err, "a CBS fetch failure must be logged, not fatal")

	buurtenLanded, err := store.Landed(BuurtenName)
	require.NoError(t, err)
	assert.True(t, buurtenLanded, "buurten should land despite the CBS failure")

	wijkenLanded, err := store.Landed(WijkenName)
	require.NoError(t, err)
	assert.True(t, wijkenLanded, "wijken should land despite the CBS failure")

	cbsLanded, err := store.Landed(CBSName)
	require.NoError(t, err)
	assert.False(t, cbsLanded, "CBS artifact should not be landed when its fetch failed")

	assert.Contains(t, calls, cbsURL, "the CBS WFS URL should have been attempted")
}

// TestIngest_SkipsAlreadyLandedArtifacts asserts the idempotent skip: once buurten, wijken, and
// CBS are all landed, Ingest must not invoke httpGet again for any of them.
func TestIngest_SkipsAlreadyLandedArtifacts(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())
	for _, name := range []string{BuurtenName, WijkenName, CBSName} {
		_, err := store.Land(name, strings.NewReader("{}"), "https://example.com/prior", time.Now())
		require.NoError(t, err)
	}

	calls := 0
	fakeGet := func(_ context.Context, _ string, _ map[string]string) (io.ReadCloser, error) {
		calls++
		return nil, errors.New("httpGet must not be called when everything is already landed")
	}

	err := Ingest(context.Background(), store, fakeGet)
	require.NoError(t, err)
	assert.Equal(t, 0, calls, "httpGet must not be invoked for already-landed artifacts")
}
