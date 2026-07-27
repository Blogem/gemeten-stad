package koop

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Blogem/gemeten-stad/location"
)

// -- 1.5 parseRecord ---------------------------------------------------------------

// TestParseRecord_Besluit covers the happy path (acceptance scenario 1): a realistic besluit
// record fragment (testdata/besluit_record.xml, the design-doc example title, docs/DATA_SOURCES.md
// / docs/DATA_THREAD_TREES.md) must yield the correct title, available date, activiteit, and RD
// point. The fixture is the inner XML of <srw:record> with no xmlns declarations, so this also
// pins that parseRecord matches elements by local name only, ignoring the (unbound) prefixes.
func TestParseRecord_Besluit(t *testing.T) {
	raw, err := os.ReadFile("testdata/besluit_record.xml")
	require.NoError(t, err)

	title, available, activiteit, point, err := parseRecord(raw)
	require.NoError(t, err)

	assert.Equal(t, "Besluit omgevingsvergunning vellen van een houtopstand (kap) reguliere procedure Örehof 8 1024BB Amsterdam", title)
	assert.Equal(t, "2022-05-31", available)
	assert.Equal(t, "kappen", activiteit)
	require.NotNil(t, point, "besluit_record.xml carries a POINT geometrie")
	assert.Equal(t, &location.RDPoint{X: 122000, Y: 490000}, point)
}

// TestParseRecord_Aanvraag covers the aanvraag counterpart of the same address/zaak, earlier in
// the process (available before the besluit's).
func TestParseRecord_Aanvraag(t *testing.T) {
	raw, err := os.ReadFile("testdata/aanvraag_record.xml")
	require.NoError(t, err)

	title, available, activiteit, point, err := parseRecord(raw)
	require.NoError(t, err)

	assert.Equal(t, "Aanvraag omgevingsvergunning vellen van een houtopstand (kap) reguliere procedure Örehof 8 1024BB Amsterdam", title)
	assert.Equal(t, "2022-04-15", available)
	assert.Equal(t, "kappen", activiteit)
	require.NotNil(t, point)
	assert.Equal(t, &location.RDPoint{X: 122000, Y: 490000}, point)
}

// TestParseRecord_MissingGeometrie covers acceptance scenario 2 (first half): a record whose
// geometrie element is absent entirely must yield point == nil and no error — never panic. Title,
// available, and activiteit are all still present and must still parse correctly.
func TestParseRecord_MissingGeometrie(t *testing.T) {
	raw, err := os.ReadFile("testdata/record_missing_geometrie.xml")
	require.NoError(t, err)

	var (
		title, available, activiteit string
		point                        *location.RDPoint
	)
	require.NotPanics(t, func() {
		title, available, activiteit, point, err = parseRecord(raw)
	})
	require.NoError(t, err)

	assert.Equal(t, "Besluit omgevingsvergunning vellen van een houtopstand (kap) reguliere procedure Twiskeweg 9 1034AB Amsterdam", title)
	assert.Equal(t, "2022-06-10", available)
	assert.Equal(t, "kappen", activiteit)
	assert.Nil(t, point, "no geometrie element must yield point == nil, not a zero-valued point or panic")
}

// TestParseRecord_MissingOptionalFields covers acceptance scenario 2 (second half): a record with
// only a title (no available, no activiteit, no geometrie) must yield empty strings and a nil
// point, no error, never panic — the "flag, don't drop" defensive path.
func TestParseRecord_MissingOptionalFields(t *testing.T) {
	raw, err := os.ReadFile("testdata/record_missing_fields.xml")
	require.NoError(t, err)

	var (
		title, available, activiteit string
		point                        *location.RDPoint
	)
	require.NotPanics(t, func() {
		title, available, activiteit, point, err = parseRecord(raw)
	})
	require.NoError(t, err)

	assert.Equal(t, "Rectificatie kapvergunning diverse locaties Amsterdam-Noord", title)
	assert.Empty(t, available, "missing dcterms:available must be empty, not an error")
	assert.Empty(t, activiteit, "missing overheidop:activiteit must be empty, not an error")
	assert.Nil(t, point)
}

// -- 1.5 parseZaaknummer ------------------------------------------------------------

// TestParseZaaknummer_Present covers acceptance scenario 3: the metadata.xml sidecar's
// OVERHEIDop.referentienummer entry is the zaaknummer (prefix "N" = stadsdeel Noord, per
// docs/DATA_SOURCES.md / spikes/spike-b).
func TestParseZaaknummer_Present(t *testing.T) {
	raw, err := os.ReadFile("testdata/metadata_sidecar.xml")
	require.NoError(t, err)

	zaaknummer, err := parseZaaknummer(raw)
	require.NoError(t, err)
	assert.Equal(t, "Z2022-N002608", zaaknummer)
}

// TestParseZaaknummer_Absent covers acceptance scenario 3's defensive half: a sidecar with no
// OVERHEIDop.referentienummer entry must yield "" and no error, never panic — thin aanvraag stubs
// are not expected to always carry a zaaknummer.
func TestParseZaaknummer_Absent(t *testing.T) {
	raw, err := os.ReadFile("testdata/metadata_sidecar_no_referentienummer.xml")
	require.NoError(t, err)

	var zaaknummer string
	require.NotPanics(t, func() {
		zaaknummer, err = parseZaaknummer(raw)
	})
	require.NoError(t, err)
	assert.Empty(t, zaaknummer)
}

// -- 1.5 classifyKind ---------------------------------------------------------------

// TestClassifyKind covers acceptance scenario 4: every documented title prefix, including the
// Ontwerpbesluit/Besluit prefix collision (a naive substring/Contains check on "besluit" would
// misclassify "Ontwerpbesluit ..." as KindBesluit — it must not) and case-insensitivity.
func TestClassifyKind(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  Kind
	}{
		{
			name:  "aanvraag prefix",
			title: "Aanvraag omgevingsvergunning kappen Twiskeweg 9, Amsterdam-Noord",
			want:  KindAanvraag,
		},
		{
			name:  "besluit prefix",
			title: "Besluit omgevingsvergunning vellen van een houtopstand (kap) Örehof 8 1024BB Amsterdam",
			want:  KindBesluit,
		},
		{
			name:  "ontwerpbesluit prefix must not collide with besluit",
			title: "Ontwerpbesluit omgevingsvergunning kappen Twiskeweg 9, Amsterdam-Noord",
			want:  KindOntwerpbesluit,
		},
		{
			name:  "verlenging prefix",
			title: "Verlenging beslistermijn omgevingsvergunning kappen Twiskeweg 9",
			want:  KindVerlenging,
		},
		{
			name:  "ingetrokken prefix",
			title: "Ingetrokken aanvraag omgevingsvergunning kappen Twiskeweg 9",
			want:  KindIngetrokken,
		},
		{
			name:  "unrecognized prefix falls back to other",
			title: "Rectificatie kapvergunning diverse locaties Amsterdam-Noord",
			want:  KindOther,
		},
		{
			name:  "case-insensitive besluit",
			title: "besluit omgevingsvergunning kappen Twiskeweg 9, Amsterdam-Noord",
			want:  KindBesluit,
		},
		{
			name:  "case-insensitive ontwerpbesluit still does not collide with besluit",
			title: "ONTWERPBESLUIT omgevingsvergunning kappen Twiskeweg 9",
			want:  KindOntwerpbesluit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, classifyKind(tt.title))
		})
	}
}

// -- 1.5 extractAddress ---------------------------------------------------------------

// TestExtractAddress covers acceptance scenario 5: the design-doc example address, a title with
// no postcode at all (defensive path), and a spaced postcode ("1024 BB") normalizing to "1024BB".
func TestExtractAddress(t *testing.T) {
	tests := []struct {
		name           string
		title          string
		wantPostcode   string
		wantHuisnummer int
		wantStreetHas  string
	}{
		{
			name:           "design-doc example address",
			title:          "Besluit omgevingsvergunning vellen van een houtopstand (kap) reguliere procedure Örehof 8 1024BB Amsterdam",
			wantPostcode:   "1024BB",
			wantHuisnummer: 8,
			wantStreetHas:  "Örehof",
		},
		{
			name:           "spaced postcode normalizes to compact form",
			title:          "Besluit omgevingsvergunning kappen Twiskeweg 9 1024 BB Amsterdam",
			wantPostcode:   "1024BB",
			wantHuisnummer: 9,
			wantStreetHas:  "Twiskeweg",
		},
		{
			name:           "no postcode in title yields all zero values",
			title:          "Rectificatie kapvergunning diverse locaties Amsterdam-Noord",
			wantPostcode:   "",
			wantHuisnummer: 0,
			wantStreetHas:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			postcode, huisnummer, street := extractAddress(tt.title)
			assert.Equal(t, tt.wantPostcode, postcode)
			assert.Equal(t, tt.wantHuisnummer, huisnummer)
			if tt.wantStreetHas == "" {
				assert.Empty(t, street)
			} else {
				assert.Contains(t, street, tt.wantStreetHas)
			}
		})
	}
}
