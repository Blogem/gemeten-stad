package shared

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestRawStore_Land_WritesArtifactAndProvenance covers acceptance scenario 1:
// landing an artifact for the first time captures its bytes verbatim (proven
// via ByteSize + ContentSHA256) and echoes the caller-supplied source URL and
// fetch time back in the returned Provenance.
func TestRawStore_Land_WritesArtifactAndProvenance(t *testing.T) {
	dir := t.TempDir()
	store := NewRawStore(dir)

	content := []byte("<gml>bomen-noord payload bytes</gml>")
	sourceURL := "https://api.data.amsterdam.nl/v1/wfs/bomen/?bbox=noord"
	fetchedAt := time.Date(2026, 7, 25, 10, 30, 0, 0, time.UTC)

	prov, err := store.Land("bomen-noord", bytes.NewReader(content), sourceURL, fetchedAt)
	require.NoError(t, err)

	assert.Equal(t, sourceURL, prov.SourceURL)
	assert.True(t, fetchedAt.Equal(prov.FetchedAt), "FetchedAt = %v, want %v", prov.FetchedAt, fetchedAt)
	assert.Equal(t, int64(len(content)), prov.ByteSize)
	assert.NotEmpty(t, prov.ContentSHA256)
	assert.Equal(t, sha256Hex(content), prov.ContentSHA256, "ContentSHA256 must match sha256 of the exact bytes landed")
}

// TestRawStore_Landed covers acceptance scenario 2: the idempotency check
// reports false before an artifact has been landed and true afterward.
func TestRawStore_Landed(t *testing.T) {
	dir := t.TempDir()
	store := NewRawStore(dir)

	landed, err := store.Landed("bomen-noord")
	require.NoError(t, err)
	assert.False(t, landed, "artifact must not be reported landed before Land is called")

	_, err = store.Land("bomen-noord", strings.NewReader("payload"), "https://example.org/bomen.gml", time.Now())
	require.NoError(t, err)

	landed, err = store.Landed("bomen-noord")
	require.NoError(t, err)
	assert.True(t, landed, "artifact must be reported landed after Land succeeds")
}

// TestRawStore_Landed_DifferentArtifactsAreIndependent guards against a
// landing check keying off the wrong identifier (e.g. always true once
// anything has been landed).
func TestRawStore_Landed_DifferentArtifactsAreIndependent(t *testing.T) {
	dir := t.TempDir()
	store := NewRawStore(dir)

	_, err := store.Land("bomen-noord", strings.NewReader("payload"), "https://example.org/bomen.gml", time.Now())
	require.NoError(t, err)

	landed, err := store.Landed("bomen-zuid")
	require.NoError(t, err)
	assert.False(t, landed, "landing one artifact must not mark a differently-named artifact as landed")
}

// TestRawStore_ProvenanceHistory_PreservedAcrossRefresh covers acceptance
// scenario 3: re-landing the same artifact name (e.g. a refreshed WFS pull)
// must not erase the prior provenance record — history accumulates, oldest
// first.
func TestRawStore_ProvenanceHistory_PreservedAcrossRefresh(t *testing.T) {
	dir := t.TempDir()
	store := NewRawStore(dir)

	const name = "bomen-noord"

	firstURL := "https://example.org/bomen-noord/v1.gml"
	firstAt := time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
	firstContent := []byte("first version of the artifact")

	secondURL := "https://example.org/bomen-noord/v2.gml"
	secondAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	secondContent := []byte("second version of the artifact, refreshed and different")

	firstProv, err := store.Land(name, bytes.NewReader(firstContent), firstURL, firstAt)
	require.NoError(t, err)

	secondProv, err := store.Land(name, bytes.NewReader(secondContent), secondURL, secondAt)
	require.NoError(t, err)

	// Sanity: the two landings really did produce distinct content digests.
	require.NotEqual(t, firstProv.ContentSHA256, secondProv.ContentSHA256)

	history, err := store.ProvenanceHistory(name)
	require.NoError(t, err)
	require.Len(t, history, 2, "history must retain both landings, not just the latest")

	assert.Equal(t, firstProv.SourceURL, history[0].SourceURL)
	assert.True(t, firstAt.Equal(history[0].FetchedAt))
	assert.Equal(t, firstProv.ByteSize, history[0].ByteSize)
	assert.Equal(t, firstProv.ContentSHA256, history[0].ContentSHA256)

	assert.Equal(t, secondProv.SourceURL, history[1].SourceURL)
	assert.True(t, secondAt.Equal(history[1].FetchedAt))
	assert.Equal(t, secondProv.ByteSize, history[1].ByteSize)
	assert.Equal(t, secondProv.ContentSHA256, history[1].ContentSHA256)

	// Landed must still report true after a refresh.
	landed, err := store.Landed(name)
	require.NoError(t, err)
	assert.True(t, landed)
}

// TestRawStore_ProvenanceHistory_ThreeGenerations extends the refresh
// scenario to a third landing to guard against an implementation that only
// keeps a "previous" pointer (capacity 2) rather than a full history.
func TestRawStore_ProvenanceHistory_ThreeGenerations(t *testing.T) {
	dir := t.TempDir()
	store := NewRawStore(dir)

	const name = "bomen-noord"
	times := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}
	contents := [][]byte{
		[]byte("generation one"),
		[]byte("generation two"),
		[]byte("generation three"),
	}

	for i := range times {
		_, err := store.Land(name, bytes.NewReader(contents[i]), "https://example.org/gen", times[i])
		require.NoError(t, err)
	}

	history, err := store.ProvenanceHistory(name)
	require.NoError(t, err)
	require.Len(t, history, 3)

	for i := range times {
		assert.True(t, times[i].Equal(history[i].FetchedAt), "history[%d].FetchedAt = %v, want %v", i, history[i].FetchedAt, times[i])
		assert.Equal(t, sha256Hex(contents[i]), history[i].ContentSHA256)
	}
}

// TestProvenance_JSONRoundTrip is a light sanity check that Provenance is
// actually JSON-tagged (per the contract) and round-trips cleanly, since
// provenance records get persisted/serialized as part of the landing store.
func TestProvenance_JSONRoundTrip(t *testing.T) {
	prov := Provenance{
		SourceURL:     "https://example.org/bomen-noord.gml",
		FetchedAt:     time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
		ByteSize:      4096,
		ContentSHA256: sha256Hex([]byte("some content")),
	}

	data, err := json.Marshal(prov)
	require.NoError(t, err)

	var roundTripped Provenance
	require.NoError(t, json.Unmarshal(data, &roundTripped))

	assert.Equal(t, prov.SourceURL, roundTripped.SourceURL)
	assert.True(t, prov.FetchedAt.Equal(roundTripped.FetchedAt))
	assert.Equal(t, prov.ByteSize, roundTripped.ByteSize)
	assert.Equal(t, prov.ContentSHA256, roundTripped.ContentSHA256)
}

// TestRawStore_Land_RejectsPathEscape covers the path-containment security
// fix: Land must reject (and write nothing for) a name that resolves outside
// BasePath, whether via a leading ".." or a nested "prefix/../../" escape,
// while still allowing legitimate flat and nested names to land normally.
func TestRawStore_Land_RejectsPathEscape(t *testing.T) {
	tests := []struct {
		name       string
		artifactID string
		wantErr    bool
	}{
		{name: "leading parent traversal", artifactID: "../escape.xml", wantErr: true},
		{name: "nested double parent traversal", artifactID: "koop/../../escape.xml", wantErr: true},
		{name: "legitimate flat name", artifactID: "bomen_kapenherplant", wantErr: false},
		{name: "legitimate nested name", artifactID: "koop/gmb-2022-1.xml", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			store := NewRawStore(dir)
			content := []byte("payload bytes for path-escape test")

			prov, err := store.Land(tt.artifactID, bytes.NewReader(content), "https://example.org/x", time.Now())

			if tt.wantErr {
				require.Error(t, err, "Land(%q, ...) must reject a name that resolves outside BasePath", tt.artifactID)
				assert.Equal(t, Provenance{}, prov, "a rejected Land call must return a zero-value Provenance")

				escaped := filepath.Join(dir, tt.artifactID)
				_, statErr := os.Stat(escaped)
				assert.True(t, os.IsNotExist(statErr),
					"Land must not write any file for escaping name %q, got stat err = %v", tt.artifactID, statErr)

				entries, rdErr := os.ReadDir(dir)
				require.NoError(t, rdErr)
				assert.Empty(t, entries, "Land must leave BasePath untouched when it rejects an escaping name")
				return
			}

			require.NoError(t, err, "Land(%q, ...) must succeed for a legitimate name", tt.artifactID)
			assert.Equal(t, sha256Hex(content), prov.ContentSHA256)

			gotBytes, err := os.ReadFile(filepath.Join(dir, tt.artifactID))
			require.NoError(t, err)
			assert.Equal(t, content, gotBytes, "the landed artifact bytes must match the content passed to Land")
		})
	}
}

// TestRawDataPath covers acceptance scenario 4: GS_RAW_DATA_PATH is read from
// an injected env closure, returning the configured path when set and an
// error when unset or blank.
func TestRawDataPath(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantPath string
		wantErr  bool
	}{
		{
			name:     "set",
			env:      map[string]string{"GS_RAW_DATA_PATH": "/var/lib/gemeten-stad/raw"},
			wantPath: "/var/lib/gemeten-stad/raw",
		},
		{
			name:    "unset",
			env:     map[string]string{},
			wantErr: true,
		},
		{
			name:    "empty",
			env:     map[string]string{"GS_RAW_DATA_PATH": ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := func(k string) string { return tt.env[k] }

			got, err := RawDataPath(env)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, got)
		})
	}
}
