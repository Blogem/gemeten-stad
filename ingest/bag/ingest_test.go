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

// fakeAtomFeed is a minimal PDOK-shaped atom feed carrying a single .zip download link, enough
// for the real ParseAtomFeed to resolve a download URL.
const fakeAtomFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <link rel="alternate" href="https://example.com/lvbag-extract-nl.zip" type="application/zip"/>
  </entry>
</feed>`

// TestIngest_LandsExtractOnFirstRun asserts the happy path: with nothing landed yet, Ingest
// fetches the atom feed, resolves the download URL, fetches the extract, and lands it.
func TestIngest_LandsExtractOnFirstRun(t *testing.T) {
	store := shared.NewRawStore(t.TempDir())

	calls := 0
	fakeGet := func(_ context.Context, _ string) (io.ReadCloser, error) {
		calls++
		if calls == 1 {
			return io.NopCloser(strings.NewReader(fakeAtomFeed)), nil
		}
		return io.NopCloser(strings.NewReader("fake-zip-bytes")), nil
	}

	err := Ingest(context.Background(), store, fakeGet)
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "expected one call for the feed and one for the download")

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
