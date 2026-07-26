package shared

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readSRUFixture loads a recorded SRU 2.0 searchRetrieve response from testdata.
func readSRUFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return data
}

// -- SRURequestURL ------------------------------------------------------------
//
// Covers requirement "SRU plumbing lives in the shared ingest package" / D5: the
// searchRetrieve URL/query builder must set operation=searchRetrieve, version=2.0,
// url-encode the query clause, and carry the given startRecord/maximumRecords.

func TestSRURequestURL(t *testing.T) {
	const endpoint = "https://repository.overheid.nl/sru"
	const query = `(dt.creator any "Amsterdam" AND dt.type any "omgevingsvergunning" AND cql.textAndIndexes any "kappen vellen rooien verplanten houtopstand" AND dt.available>="2021-01-01")`

	tests := []struct {
		name           string
		startRecord    int
		maximumRecords int
	}{
		{name: "first page at the default SRU page size", startRecord: 1, maximumRecords: SRUPageSize},
		{name: "second page, advanced by maximumRecords", startRecord: 101, maximumRecords: SRUPageSize},
		{name: "a smaller page size", startRecord: 5, maximumRecords: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SRURequestURL(endpoint, query, tt.startRecord, tt.maximumRecords)

			require.True(t, len(got) > 0, "SRURequestURL must not return an empty URL")
			assert.Contains(t, got, "operation=searchRetrieve", "the SRU operation must be searchRetrieve")
			assert.Contains(t, got, "version=2.0", "the SRU protocol version must be 2.0")

			u, err := url.Parse(got)
			require.NoError(t, err, "SRURequestURL must produce a parseable URL, got %q", got)
			assert.Equal(t, endpoint, u.Scheme+"://"+u.Host+u.Path, "the URL must be built against the given endpoint")

			q := u.Query()
			assert.Equal(t, "searchRetrieve", q.Get("operation"))
			assert.Equal(t, "2.0", q.Get("version"))
			assert.Equal(t, query, q.Get("query"), "the query clause must round-trip exactly through URL encoding")
			assert.Equal(t, strconv.Itoa(tt.startRecord), q.Get("startRecord"))
			assert.Equal(t, strconv.Itoa(tt.maximumRecords), q.Get("maximumRecords"))
		})
	}
}

// -- Exported constants (D5 / Resolved Questions) ----------------------------

// TestSRUConstants pins the two named tuning constants the design settled on:
// a 100-record page size (matching the proven spike-b harvest) and a ~200ms
// (~5 req/s) inter-request interval for polite sequential paging.
func TestSRUConstants(t *testing.T) {
	assert.Equal(t, 100, SRUPageSize)
	assert.Equal(t, 200*time.Millisecond, SRURateInterval)
}

// -- ParseSRUResponse ---------------------------------------------------------
//
// Covers "Results are paged to exhaustion" (numberOfRecords must be read
// correctly) and "SRU plumbing lives in the shared ingest package" (per-record
// verbatim extraction + the two cursor fields, dcterms:identifier / dt.available,
// read regardless of which XML namespace prefix or URI a given record happens to
// use for them).

func TestParseSRUResponse_ParsesEnvelopeAndRecords(t *testing.T) {
	body := readSRUFixture(t, "sru_response_two_records.xml")

	numberOfRecords, records, err := ParseSRUResponse(body)
	require.NoError(t, err)

	assert.Equal(t, 7, numberOfRecords,
		"numberOfRecords must reflect the SRU-reported total, independent of how many records are actually carried on this page")
	require.Len(t, records, 2, "both <record> elements on this page must be returned")

	first, second := records[0], records[1]

	assert.Equal(t, "gmb-2022-291126", first.Identifier)
	assert.Equal(t, "2022-06-15", first.Available)
	assert.NotEmpty(t, first.InnerXML, "InnerXML must capture the record's verbatim content")
	assert.Contains(t, string(first.InnerXML), "gmb-2022-291126",
		"InnerXML must contain the record's own identifier verbatim")

	assert.Equal(t, "gmb-2022-291127", second.Identifier,
		"identifier must be parsed from <alt:identifier> (a namespace unrelated to dcterms) by local name, not a pinned namespace URI")
	assert.Equal(t, "2022-06-16", second.Available,
		"dt.available must be parsed from <alt:dt.available> by local name, not a pinned namespace URI")
	assert.NotEmpty(t, second.InnerXML)
	assert.Contains(t, string(second.InnerXML), "gmb-2022-291127")
}

// TestParseSRUResponse_EmptyResultSet guards the "silent zero" SRU quirk (design
// Risks section): an unknown index or over-narrow clause returns numberOfRecords=0
// with no <record> elements and no error -- ParseSRUResponse must reflect that
// faithfully rather than erroring.
func TestParseSRUResponse_EmptyResultSet(t *testing.T) {
	const emptyResponse = `<?xml version="1.0" encoding="UTF-8"?>
<srw:searchRetrieveResponse xmlns:srw="http://docs.oasis-open.org/ns/search-ws/sruResponse">
  <srw:version>2.0</srw:version>
  <srw:numberOfRecords>0</srw:numberOfRecords>
</srw:searchRetrieveResponse>`

	numberOfRecords, records, err := ParseSRUResponse([]byte(emptyResponse))
	require.NoError(t, err)
	assert.Equal(t, 0, numberOfRecords)
	assert.Empty(t, records)
}

// -- FetchSRUAll ----------------------------------------------------------------
//
// Covers "Results are paged to exhaustion" and "SRU paging is exercised through
// the shared package": FetchSRUAll must keep requesting pages, driven by
// numberOfRecords, until every record has been yielded exactly once and in order,
// via an injected HTTPGetFunc (no live network access).

// fakeSRUHTTPGet dispatches recorded page fixtures by the startRecord query
// parameter of the requested URL, mirroring how a real SRU paging loop advances
// startRecord as it consumes each page -- rather than depending on the real
// SRUPageSize=100 constant, which would require 100+ record fixtures to force a
// second page.
func fakeSRUHTTPGet(t *testing.T, calls *[]string, pagesByStart map[string][]byte) HTTPGetFunc {
	return func(_ context.Context, rawURL string) (io.ReadCloser, error) {
		*calls = append(*calls, rawURL)

		u, err := url.Parse(rawURL)
		require.NoError(t, err)
		start := u.Query().Get("startRecord")

		page, ok := pagesByStart[start]
		if !ok {
			return nil, fmt.Errorf("fakeSRUHTTPGet: unexpected startRecord %q in URL %q", start, rawURL)
		}
		return io.NopCloser(bytes.NewReader(page)), nil
	}
}

func TestFetchSRUAll_PagesToExhaustion(t *testing.T) {
	// The shared package exposes a sruSleep seam (an internal, injectable stand-in
	// for the D5 rate limiter's time.Sleep) precisely so paging tests like this one
	// don't really wait SRURateInterval between fake requests.
	origSleep := sruSleep
	sruSleep = func(time.Duration) {}
	defer func() { sruSleep = origSleep }()

	page1 := readSRUFixture(t, "sru_page1.xml")
	page2 := readSRUFixture(t, "sru_page2.xml")

	var calls []string
	httpGet := fakeSRUHTTPGet(t, &calls, map[string][]byte{
		"1": page1,
		"3": page2,
	})

	var yielded []SRURecord
	numberOfRecords, err := FetchSRUAll(context.Background(), httpGet,
		"https://repository.overheid.nl/sru",
		`(dt.creator any "Amsterdam")`,
		func(r SRURecord) error {
			yielded = append(yielded, r)
			return nil
		})
	require.NoError(t, err)

	assert.Equal(t, 3, numberOfRecords, "FetchSRUAll must return the numberOfRecords reported by the SRU envelope")
	require.Len(t, yielded, 3, "every record across both pages must be yielded exactly once")

	assert.Equal(t, "gmb-2022-100001", yielded[0].Identifier)
	assert.Equal(t, "gmb-2022-100002", yielded[1].Identifier)
	assert.Equal(t, "gmb-2022-100003", yielded[2].Identifier,
		"the record from the second page must be yielded, in order, after the first page's records")

	assert.Len(t, calls, 2, "expected exactly one HTTP GET per page (2 pages: 2 records then 1)")
}

// TestFetchSRUAll_SinglePageWhenResultFitsOnePage guards the common case (a
// result set smaller than one page) against an implementation that always issues
// a second, empty-startRecord request.
func TestFetchSRUAll_SinglePageWhenResultFitsOnePage(t *testing.T) {
	origSleep := sruSleep
	sruSleep = func(time.Duration) {}
	defer func() { sruSleep = origSleep }()

	// Built inline (not from testdata) so numberOfRecords==1 matches the single
	// record actually present -- the recorded page fixtures intentionally report
	// numberOfRecords=3 to exercise multi-page paging elsewhere.
	const singlePageResponse = `<?xml version="1.0" encoding="UTF-8"?>
<srw:searchRetrieveResponse xmlns:srw="http://docs.oasis-open.org/ns/search-ws/sruResponse"
    xmlns:dcterms="http://purl.org/dc/terms/"
    xmlns:overheidwetgeving="http://standaarden.overheid.nl/owms/terms/"
    xmlns:gzd="http://standaarden.overheid.nl/sru">
  <srw:version>2.0</srw:version>
  <srw:numberOfRecords>1</srw:numberOfRecords>
  <srw:records>
    <srw:record>
      <srw:recordData>
        <gzd:gzd>
          <gzd:originalData>
            <overheidwetgeving:meta>
              <overheidwetgeving:owmskern>
                <dcterms:identifier>gmb-2022-200001</dcterms:identifier>
              </overheidwetgeving:owmskern>
              <overheidwetgeving:owmsmantel>
                <overheidwetgeving:dt.available>2022-03-01</overheidwetgeving:dt.available>
              </overheidwetgeving:owmsmantel>
            </overheidwetgeving:meta>
          </gzd:originalData>
        </gzd:gzd>
      </srw:recordData>
    </srw:record>
  </srw:records>
</srw:searchRetrieveResponse>`

	var calls []string
	httpGet := fakeSRUHTTPGet(t, &calls, map[string][]byte{
		"1": []byte(singlePageResponse),
	})

	var yielded []SRURecord
	numberOfRecords, err := FetchSRUAll(context.Background(), httpGet,
		"https://repository.overheid.nl/sru",
		`(dt.creator any "Amsterdam")`,
		func(r SRURecord) error {
			yielded = append(yielded, r)
			return nil
		})
	require.NoError(t, err)

	assert.Equal(t, 1, numberOfRecords)
	require.Len(t, yielded, 1)
	assert.Equal(t, "gmb-2022-200001", yielded[0].Identifier)
	assert.Len(t, calls, 1, "a result set that fits on one page must not trigger a second request")
}

// TestFetchSRUAll_YieldErrorStopsPaging guards against an implementation that
// swallows the yield callback's error or keeps fetching further pages after it.
func TestFetchSRUAll_YieldErrorStopsPaging(t *testing.T) {
	origSleep := sruSleep
	sruSleep = func(time.Duration) {}
	defer func() { sruSleep = origSleep }()

	page1 := readSRUFixture(t, "sru_page1.xml")
	page2 := readSRUFixture(t, "sru_page2.xml")

	var calls []string
	httpGet := fakeSRUHTTPGet(t, &calls, map[string][]byte{
		"1": page1,
		"3": page2,
	})

	wantErr := fmt.Errorf("yield: simulated downstream failure")
	_, err := FetchSRUAll(context.Background(), httpGet,
		"https://repository.overheid.nl/sru",
		`(dt.creator any "Amsterdam")`,
		func(SRURecord) error {
			return wantErr
		})

	require.Error(t, err)
	assert.Len(t, calls, 1, "must not fetch the second page once yield has returned an error on the first")
}
