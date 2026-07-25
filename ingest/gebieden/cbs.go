package gebieden

import "net/url"

// cbsWFSBase is the PDOK "wijken en buurten" WFS service, 2024 vintage (DATA_SOURCES.md §0,
// spike-d).
const cbsWFSBase = "https://service.pdok.nl/cbs/wijkenbuurten/2024/wfs/v1_0"

// CBSWFSURL builds the CBS WFS GetFeature request URL for the buurten layer, filtered to
// gemeentecode=GM0363 (whole gemeente Amsterdam), in RD (EPSG:28992), GeoJSON output. Pure —
// a cross-reference only; the gebieden polygons above remain the primary point-in-polygon set.
func CBSWFSURL() string {
	q := url.Values{}
	q.Set("service", "WFS")
	q.Set("version", "2.0.0")
	q.Set("request", "GetFeature")
	q.Set("typeNames", "wijkenbuurten:buurten")
	q.Set("outputFormat", "application/json")
	q.Set("srsName", "EPSG:28992")
	q.Set("cql_filter", "gemeentecode='GM0363'")
	return cbsWFSBase + "?" + q.Encode()
}
