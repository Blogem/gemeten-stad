package location

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- 7.3 PlaceLevelFor: the address > postcode > buurt ladder ----------------

func TestPlaceLevelFor(t *testing.T) {
	tests := []struct {
		name                              string
		hasAddress, hasPostcode, hasBuurt bool
		wantLevel                         PlaceLevel
		wantConfidence                    float64
		wantOK                            bool
	}{
		{
			name:       "address wins over postcode and buurt",
			hasAddress: true, hasPostcode: true, hasBuurt: true,
			wantLevel: PlaceLevel("address"), wantConfidence: 0.90, wantOK: true,
		},
		{
			name:       "postcode wins when address does not resolve",
			hasAddress: false, hasPostcode: true, hasBuurt: true,
			wantLevel: PlaceLevel("postcode"), wantConfidence: 0.70, wantOK: true,
		},
		{
			name:       "buurt is the floor when only PIP resolves",
			hasAddress: false, hasPostcode: false, hasBuurt: true,
			wantLevel: PlaceLevel("buurt"), wantConfidence: 0.50, wantOK: true,
		},
		{
			name:       "nothing resolves",
			hasAddress: false, hasPostcode: false, hasBuurt: false,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, confidence, ok := PlaceLevelFor(tt.hasAddress, tt.hasPostcode, tt.hasBuurt)
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			assert.Equal(t, tt.wantLevel, level)
			assert.InDelta(t, tt.wantConfidence, confidence, 1e-9)
		})
	}
}

// address-tier confidence must be exactly 0.90, not merely "close" — the
// downstream AuditLink confidence re-score depends on the literal Spike B
// place-score values.
func TestPlaceLevelFor_ExactConfidenceValues(t *testing.T) {
	_, addrConf, ok := PlaceLevelFor(true, true, true)
	require.True(t, ok)
	assert.Equal(t, 0.90, addrConf)

	_, pcConf, ok := PlaceLevelFor(false, true, true)
	require.True(t, ok)
	assert.Equal(t, 0.70, pcConf)

	_, buurtConf, ok := PlaceLevelFor(false, false, true)
	require.True(t, ok)
	assert.Equal(t, 0.50, buurtConf)
}

// -- 7.3 TimeMatchFor ----------------------------------------------------------

func TestTimeMatchFor(t *testing.T) {
	assert.Equal(t, "valid_at_date", TimeMatchFor(true))
	assert.Equal(t, "any_time", TimeMatchFor(false))
}

// -- 7.3 per-tier SQL builders --------------------------------------------------
//
// NOTE (tester assumption, pass 1): the task contract pins PlaceLevelFor and
// TimeMatchFor exactly, but only describes the per-tier SQL in prose ("funcs/
// consts you can assert fragments on") without pinning exact identifier names.
// Absent a pinned name, this file assumes three exported string constants
// mirroring spikes/spike-d/sql/resolve.sql's addr_cand / pip.sql shape:
//   AddressSQL      - the (postcode,huisnummer) + exact-street address match
//   FuzzyAddressSQL - the pg_trgm fuzzy street match
//   BuurtPIPSQL     - the ST_Contains point-in-polygon buurt match
// If the real implementation exposes different names, pass 2 must adjust
// these identifiers (not the fragments being asserted) to match.

func TestAddressSQL_ContainsRequiredFragments(t *testing.T) {
	assert.Contains(t, AddressSQL, "eindregistratie IS NULL",
		"address SQL must select the best-known voorkomen")
	assert.Contains(t, AddressSQL, "hoofdadresnummeraanduidingref",
		"address SQL must reach the adresseerbaar object via hoofdadresnummeraanduidingref")
}

func TestFuzzyAddressSQL_ContainsSimilarity(t *testing.T) {
	assert.Contains(t, FuzzyAddressSQL, "similarity(",
		"fuzzy address path must use pg_trgm similarity()")
}

func TestBuurtPIPSQL_ContainsPointInPolygonFragments(t *testing.T) {
	assert.Contains(t, BuurtPIPSQL, "ST_Contains",
		"buurt PIP SQL must use ST_Contains")
	assert.Contains(t, BuurtPIPSQL, "gebieden_buurten",
		"buurt PIP SQL must reference gebieden_buurten, never the CBS cross-reference")
}
