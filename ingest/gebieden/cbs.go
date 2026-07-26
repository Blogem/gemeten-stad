package gebieden

import "net/url"

// cbsWFSBase is the PDOK "wijken en buurten" WFS service, 2024 vintage (DATA_SOURCES.md §0,
// spike-d).
const cbsWFSBase = "https://service.pdok.nl/cbs/wijkenbuurten/2024/wfs/v1_0"

// cbsGemeenteFilter is the OGC FES 2.0 filter that narrows the buurten layer to gemeente Amsterdam
// (GM0363). This endpoint IGNORES the GeoServer `cql_filter` extension (it silently returns the
// first 1000 unfiltered features), but honours the standard WFS `FILTER` parameter — which is also
// exactly what ogr2ogr's WFS driver emits for `-where`, the form spike-d verified (519 buurten).
const cbsGemeenteFilter = `<fes:Filter xmlns:fes="http://www.opengis.net/fes/2.0">` +
	`<fes:PropertyIsEqualTo>` +
	`<fes:ValueReference>gemeentecode</fes:ValueReference>` +
	`<fes:Literal>GM0363</fes:Literal>` +
	`</fes:PropertyIsEqualTo>` +
	`</fes:Filter>`

// CBSWFSURL builds the CBS WFS GetFeature request URL for the buurten layer, filtered to
// gemeentecode=GM0363 (whole gemeente Amsterdam) via a standard OGC FES filter, in RD
// (EPSG:28992), GeoJSON output. Pure — a cross-reference only; the gebieden polygons above remain
// the primary point-in-polygon set. count=10000 comfortably covers Amsterdam's ~519 buurten in one
// page, so no WFS paging is needed.
func CBSWFSURL() string {
	q := url.Values{}
	q.Set("service", "WFS")
	q.Set("version", "2.0.0")
	q.Set("request", "GetFeature")
	q.Set("typeNames", "wijkenbuurten:buurten")
	q.Set("outputFormat", "application/json")
	q.Set("srsName", "EPSG:28992")
	q.Set("count", "10000")
	q.Set("FILTER", cbsGemeenteFilter)
	return cbsWFSBase + "?" + q.Encode()
}
