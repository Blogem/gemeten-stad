package bag

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// fakeTopFeed is a minimal PDOK-shaped top-level atom feed: its entry points to a dataset
// sub-feed rather than a download, mirroring the real two-level PDOK BAG feed.
const fakeTopFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link rel="alternate" href="https://example.com/adressen.xml" type="application/atom+xml"/>
  </entry>
</feed>`

// fakeSubFeed is a minimal PDOK-shaped dataset sub-feed carrying the actual .zip download link.
const fakeSubFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link rel="alternate" href="https://example.com/lvbag-extract-nl.zip" type="application/zip"/>
  </entry>
</feed>`

// TestIngest_LandsExtractOnFirstRun asserts the happy path: with nothing landed yet, Ingest
// fetches the top-level atom feed, follows it to the dataset sub-feed, resolves the download
// URL, fetches the extract, and lands it.
func TestIngest_LandsExtractOnFirstRun(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	calls := 0
	fakeGet := func(_ context.Context, _ string) (io.ReadCloser, error) {
		calls++
		switch calls {
		case 1:
			return io.NopCloser(strings.NewReader(fakeTopFeed)), nil
		case 2:
			return io.NopCloser(strings.NewReader(fakeSubFeed)), nil
		default:
			return io.NopCloser(strings.NewReader("fake-zip-bytes")), nil
		}
	}

	err := Ingest(context.Background(), store, fakeGet)
	require.NoError(t, err)
	assert.Equal(t, 3, calls, "expected one call for the top feed, one for the sub-feed, and one for the download")

	landed, err := store.Landed(ExtractName)
	require.NoError(t, err)
	assert.True(t, landed, "the extract should be landed after Ingest")
}

// TestIngest_SkipsWhenAlreadyLanded asserts the idempotent skip: once the extract is landed,
// Ingest must not invoke httpGet again (no feed re-fetch, no re-download).
func TestIngest_SkipsWhenAlreadyLanded(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())
	_, err := store.Land(ExtractName, strings.NewReader("already-here"), "https://example.com/prior", time.Now())
	require.NoError(t, err)

	calls := 0
	fakeGet := func(_ context.Context, _ string) (io.ReadCloser, error) {
		calls++
		return nil, errors.New("httpGet must not be called when already landed")
	}

	err = Ingest(context.Background(), store, fakeGet)
	require.NoError(t, err)
	assert.Equal(t, 0, calls, "httpGet must not be invoked when the extract is already landed")
}

// TestIngest_ErrorsWhenFeedHopsExhausted asserts that a feed which never bottoms out in a
// download link (e.g. a chain of sub-feeds longer than maxFeedHops, or a sub-feed loop) is
// reported as an error rather than looping forever.
func TestIngest_ErrorsWhenFeedHopsExhausted(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	// Every response is a sub-feed link pointing at itself: it never resolves to a download.
	const foreverSubFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link rel="alternate" href="https://example.com/adressen.xml" type="application/atom+xml"/>
  </entry>
</feed>`

	calls := 0
	fakeGet := func(_ context.Context, _ string) (io.ReadCloser, error) {
		calls++
		return io.NopCloser(strings.NewReader(foreverSubFeed)), nil
	}

	err := Ingest(context.Background(), store, fakeGet)
	require.Error(t, err)
	assert.Equal(t, maxFeedHops, calls, "must stop after exactly the hop budget, never loop forever")

	landed, landedErr := store.Landed(ExtractName)
	require.NoError(t, landedErr)
	assert.False(t, landed, "nothing should be landed when hops are exhausted")
}
