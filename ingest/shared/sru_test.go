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

// TestSRUConstants pins the named tuning constants the design settled on: a
// 100-record page size (matching the proven spike-b harvest), a ~200ms (~5
// req/s) inter-request interval for polite sequential paging, and the P11
// live-harvest retry tuning (8 attempts, 1s exponential backoff base capped
// at 30s) that keeps a single transient httpGet/read failure from aborting a
// page fetch.
func TestSRUConstants(t *testing.T) {
	assert.Equal(t, 100, SRUPageSize)
	assert.Equal(t, 200*time.Millisecond, SRURateInterval)
	assert.Equal(t, 8, SRUMaxAttempts)
	assert.Equal(t, 1*time.Second, SRURetryBaseDelay)
	assert.Equal(t, 30*time.Second, SRUMaxRetryDelay)
}

// -- ParseSRUResponse ---------------------------------------------------------
//
// Covers "Results are paged to exhaustion" (numberOfRecords must be read
// correctly) and "SRU plumbing lives in the shared ingest package" (per-record
// verbatim extraction + the two cursor fields, dcterms:identifier / dcterms:available,
// read regardless of which XML namespace prefix or URI a given record happens to
// use for them -- matched by local element name only: "identifier" and
// "available", NOT "dt.available").

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
		"available must be parsed from <alt:available> (local name \"available\", not \"dt.available\") by local name, not a pinned namespace URI")
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
                <dcterms:available>2022-03-01</dcterms:available>
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

// -- FetchSRUAll retry behavior -------------------------------------------------
//
// Covers the P11 live-harvest fix: a single flaky httpGet/body-read failure on
// a page must be retried (with exponential backoff via the sruSleep seam, per
// min(SRURetryBaseDelay << (n-1), SRUMaxRetryDelay)) up to SRUMaxAttempts
// before giving up, so one transient network blip doesn't abort an otherwise-
// successful harvest run. An XML parse error is a distinct failure mode (a
// malformed response body, not a transient transport error) and is
// deliberately NOT retried -- these tests only exercise the httpGet/body-read
// failure path.

// flakyHTTPGet returns transportErr for the first failCount calls (tracked via
// *calls) and serves page for every call after that.
func flakyHTTPGet(calls *int, failCount int, page []byte, transportErr error) HTTPGetFunc {
	return func(_ context.Context, _ string) (io.ReadCloser, error) {
		*calls++
		if *calls <= failCount {
			return nil, transportErr
		}
		return io.NopCloser(bytes.NewReader(page)), nil
	}
}

// singlePageAvailableFixture is a minimal one-record, one-page SRU response
// (numberOfRecords matches the single carried record, so FetchSRUAll doesn't
// need a second page) used purely to prove the retry-then-succeed path yields
// the record once the flaky getter finally returns a good response.
const singlePageAvailableFixture = `<?xml version="1.0" encoding="UTF-8"?>
<srw:searchRetrieveResponse xmlns:srw="http://docs.oasis-open.org/ns/search-ws/sruResponse"
    xmlns:dcterms="http://purl.org/dc/terms/"
    xmlns:gzd="http://standaarden.overheid.nl/sru">
  <srw:version>2.0</srw:version>
  <srw:numberOfRecords>1</srw:numberOfRecords>
  <srw:records>
    <srw:record>
      <srw:recordData>
        <gzd:gzd>
          <dcterms:identifier>gmb-2022-500001</dcterms:identifier>
          <dcterms:available>2022-07-01</dcterms:available>
        </gzd:gzd>
      </srw:recordData>
    </srw:record>
  </srw:records>
</srw:searchRetrieveResponse>`

// TestFetchSRUAll_RetriesTransientFailures guards against an implementation
// that gives up on the first httpGet/read error: with a getter that fails
// twice (well under SRUMaxAttempts) and then succeeds, FetchSRUAll must
// still yield the page's record and return no error.
func TestFetchSRUAll_RetriesTransientFailures(t *testing.T) {
	origSleep := sruSleep
	sruSleep = func(time.Duration) {}
	defer func() { sruSleep = origSleep }()

	var calls int
	const failCount = 2 // < SRUMaxAttempts
	httpGet := flakyHTTPGet(&calls, failCount, []byte(singlePageAvailableFixture),
		fmt.Errorf("transient: connection reset"))

	var yielded []SRURecord
	numberOfRecords, err := FetchSRUAll(context.Background(), httpGet,
		"https://repository.overheid.nl/sru",
		`(dt.creator any "Amsterdam")`,
		func(r SRURecord) error {
			yielded = append(yielded, r)
			return nil
		})

	require.NoError(t, err, "FetchSRUAll must succeed once a retry within SRUMaxAttempts gets a good response")
	assert.Equal(t, 1, numberOfRecords)
	require.Len(t, yielded, 1, "the record from the eventually-successful attempt must still be yielded")
	assert.Equal(t, "gmb-2022-500001", yielded[0].Identifier)
	assert.Equal(t, failCount+1, calls,
		"httpGet must be called exactly until the first success: %d failures + 1 success", failCount)
}

// TestFetchSRUAll_ExhaustsRetriesAndReturnsError guards against an
// implementation that retries forever (or not at all) rather than giving up
// after exactly SRUMaxAttempts failed attempts and surfacing the last error.
func TestFetchSRUAll_ExhaustsRetriesAndReturnsError(t *testing.T) {
	origSleep := sruSleep
	sruSleep = func(time.Duration) {}
	defer func() { sruSleep = origSleep }()

	var calls int
	wantErr := fmt.Errorf("transient: connection reset")
	// failCount == SRUMaxAttempts: every attempt fails, so the getter never
	// serves a page -- proving FetchSRUAll gives up after SRUMaxAttempts
	// attempts rather than retrying indefinitely.
	httpGet := flakyHTTPGet(&calls, SRUMaxAttempts, nil, wantErr)

	_, err := FetchSRUAll(context.Background(), httpGet,
		"https://repository.overheid.nl/sru",
		`(dt.creator any "Amsterdam")`,
		func(SRURecord) error { return nil })

	require.Error(t, err, "FetchSRUAll must return an error once every attempt for a page has failed")
	assert.Equal(t, SRUMaxAttempts, calls,
		"httpGet must be attempted exactly SRUMaxAttempts times before giving up")
}

// TestFetchSRUAll_BackoffIsCappedExponential guards the capped exponential
// backoff schedule directly: it records every duration passed to the
// sruSleep seam (rather than stubbing it to a no-op) while a page fetch
// fails all SRUMaxAttempts attempts, so the only sleeps captured are the
// retry backoffs -- there is no successful page (so no second page, hence no
// SRURateInterval inter-page pacing sleep) and no sleep before the very first
// request (FetchSRUAll never sleeps before its first fetch). This proves both
// the doubling (1s, 2s, 4s, 8s, 16s) and the SRUMaxRetryDelay=30s cap
// (32s/64s would be the uncapped values for attempts 6-7).
func TestFetchSRUAll_BackoffIsCappedExponential(t *testing.T) {
	origSleep := sruSleep
	var recorded []time.Duration
	sruSleep = func(d time.Duration) { recorded = append(recorded, d) }
	defer func() { sruSleep = origSleep }()

	var calls int
	wantErr := fmt.Errorf("transient: connection reset")
	// failCount == SRUMaxAttempts: every attempt fails, so exactly
	// SRUMaxAttempts-1 backoff sleeps are recorded (no sleep after the final
	// failed attempt) and no page is ever successfully fetched.
	httpGet := flakyHTTPGet(&calls, SRUMaxAttempts, nil, wantErr)

	_, err := FetchSRUAll(context.Background(), httpGet,
		"https://repository.overheid.nl/sru",
		`(dt.creator any "Amsterdam")`,
		func(SRURecord) error { return nil })

	require.Error(t, err)
	assert.Equal(t, SRUMaxAttempts, calls)
	assert.Equal(t, []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}, recorded, "backoff after each failed attempt must double from SRURetryBaseDelay, capping at SRUMaxRetryDelay once the doubled value would exceed it")
}
