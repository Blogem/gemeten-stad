package koop

import "fmt"

// SRUEndpoint is the public KOOP SRU 2.0 searchRetrieve endpoint (no auth required).
const SRUEndpoint = "https://repository.overheid.nl/sru"

// DefaultSinceDate is the dt.available lower bound used when no cursor has been persisted yet
// (D1/D3): a first run on a clean landing store walks the full 2021-01-01 -> present window.
const DefaultSinceDate = "2021-01-01"

// BuildQuery builds the D1 scoped Amsterdam kap/verplant SRU query: publishing authority
// Amsterdam (dt.creator any "Amsterdam"), document type omgevingsvergunning (dt.type any
// "omgevingsvergunning"), the kap/verplant full-text term set (cql.textAndIndexes any), and a
// dt.available lower bound of sinceDate. This is exactly the spike-b query shape with the year
// UPPER bound dropped (2021 -> present, not a single year) and no stadsdeel/geometry/postcode
// clause — the harvest is deliberately Amsterdam-wide; Noord selection is deferred to P13's
// authoritative BAG location resolution. Pure: no I/O.
func BuildQuery(sinceDate string) string {
	return fmt.Sprintf(
		`(dt.creator any "Amsterdam" AND dt.type any "omgevingsvergunning" AND cql.textAndIndexes any "kappen vellen rooien verplanten houtopstand" AND dt.available>="%s")`,
		sinceDate,
	)
}
