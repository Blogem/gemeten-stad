package koop

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Publication literals below are hand-built (not parsed from fixtures) since selectBesluit and
// groupByZaak operate purely on already-parsed Publication values — the dedup/selection logic is
// what's under test here, independent of task 1.5's XML parsing.

// -- 2.2 selectBesluit ---------------------------------------------------------------

// TestSelectBesluit_AanvraagAndBesluit covers acceptance scenario 6: given both an aanvraag and a
// besluit for the same zaak, selectBesluit must return the besluit with ok == true.
func TestSelectBesluit_AanvraagAndBesluit(t *testing.T) {
	aanvraag := Publication{ID: "gmb-2022-198734", Zaaknummer: "Z2022-N002608", Kind: KindAanvraag, Available: "2022-04-15"}
	besluit := Publication{ID: "gmb-2022-245014", Zaaknummer: "Z2022-N002608", Kind: KindBesluit, Available: "2022-05-31"}

	got, ok := selectBesluit([]Publication{aanvraag, besluit})
	assert.True(t, ok)
	assert.Equal(t, besluit.ID, got.ID)
	assert.Equal(t, KindBesluit, got.Kind)
}

// TestSelectBesluit_AanvraagOnly covers acceptance scenario 7: a zaak with only an aanvraag (no
// besluit yet) is still pending — selectBesluit must return ok == false, so no Intervention is
// assembled for it.
func TestSelectBesluit_AanvraagOnly(t *testing.T) {
	aanvraag := Publication{ID: "gmb-2022-198734", Zaaknummer: "Z2022-N002608", Kind: KindAanvraag, Available: "2022-04-15"}

	_, ok := selectBesluit([]Publication{aanvraag})
	assert.False(t, ok, "a zaak with only an aanvraag is pending, not yet decided")
}

// TestSelectBesluit_MultipleBesluiten covers acceptance scenario 8: when a zaak carries more than
// one besluit publication (e.g. a corrected/republished besluit), selectBesluit picks the one with
// the latest Available date — the most recent decision supersedes an earlier one for the same
// zaak.
func TestSelectBesluit_MultipleBesluiten(t *testing.T) {
	earlier := Publication{ID: "gmb-2022-245014", Zaaknummer: "Z2022-N002608", Kind: KindBesluit, Available: "2022-05-31"}
	later := Publication{ID: "gmb-2022-245099", Zaaknummer: "Z2022-N002608", Kind: KindBesluit, Available: "2022-06-15"}

	got, ok := selectBesluit([]Publication{earlier, later})
	assert.True(t, ok)
	assert.Equal(t, later.ID, got.ID, "the besluit with the latest Available date must win")
}

// TestSelectBesluit_OnlyOntwerpbesluit covers acceptance scenario 8: a zaak with only an
// ontwerpbesluit (draft decision, no besluit yet published) still yields a result — ontwerpbesluit
// is the best available signal until a real besluit appears.
func TestSelectBesluit_OnlyOntwerpbesluit(t *testing.T) {
	ontwerp := Publication{ID: "gmb-2022-300111", Zaaknummer: "Z2022-N003000", Kind: KindOntwerpbesluit, Available: "2022-05-01"}

	got, ok := selectBesluit([]Publication{ontwerp})
	assert.True(t, ok)
	assert.Equal(t, ontwerp.ID, got.ID)
	assert.Equal(t, KindOntwerpbesluit, got.Kind)
}

// TestSelectBesluit_BesluitPreferredOverOntwerpbesluit covers acceptance scenario 8: when a zaak
// carries both an ontwerpbesluit and a besluit, the (final) besluit is preferred over the draft,
// regardless of Available ordering.
func TestSelectBesluit_BesluitPreferredOverOntwerpbesluit(t *testing.T) {
	ontwerp := Publication{ID: "gmb-2022-300111", Zaaknummer: "Z2022-N003000", Kind: KindOntwerpbesluit, Available: "2022-05-01"}
	besluit := Publication{ID: "gmb-2022-300222", Zaaknummer: "Z2022-N003000", Kind: KindBesluit, Available: "2022-06-01"}

	got, ok := selectBesluit([]Publication{ontwerp, besluit})
	assert.True(t, ok)
	assert.Equal(t, besluit.ID, got.ID, "a final besluit must be preferred over a draft ontwerpbesluit")
	assert.Equal(t, KindBesluit, got.Kind)
}

// -- 2.2 groupByZaak ---------------------------------------------------------------

// TestGroupByZaak_MixedSlice covers acceptance scenario 9: publications must group correctly by
// Zaaknummer, and publications with no zaaknummer at all (Zaaknummer == "") must still be
// retrievable under the "" bucket — they are kept for the audit trail, never audited themselves,
// but must never be silently dropped.
func TestGroupByZaak_MixedSlice(t *testing.T) {
	aanvraagA := Publication{ID: "gmb-2022-198734", Zaaknummer: "Z2022-N002608"}
	besluitA := Publication{ID: "gmb-2022-245014", Zaaknummer: "Z2022-N002608"}
	besluitB := Publication{ID: "gmb-2022-300222", Zaaknummer: "Z2022-N003000"}
	keyless := Publication{ID: "gmb-2022-999999", Zaaknummer: ""}

	groups := groupByZaak([]Publication{aanvraagA, besluitA, besluitB, keyless})

	if assert.Contains(t, groups, "Z2022-N002608") {
		assert.Len(t, groups["Z2022-N002608"], 2)
		var ids []string
		for _, p := range groups["Z2022-N002608"] {
			ids = append(ids, p.ID)
		}
		assert.ElementsMatch(t, []string{"gmb-2022-198734", "gmb-2022-245014"}, ids)
	}

	if assert.Contains(t, groups, "Z2022-N003000") {
		assert.Len(t, groups["Z2022-N003000"], 1)
		assert.Equal(t, "gmb-2022-300222", groups["Z2022-N003000"][0].ID)
	}

	if assert.Contains(t, groups, "", "keyless publications must be kept, not dropped") {
		assert.Len(t, groups[""], 1)
		assert.Equal(t, "gmb-2022-999999", groups[""][0].ID)
	}
}
