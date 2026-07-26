package bomen

// Resolved-via outcomes for the kapenherplant -> stamgegevens point resolution (design.md D3,
// specs/bomen-load/spec.md).
const (
	ResolvedViaBoomID      = "boomId"
	ResolvedViaBoomNieuwID = "boomNieuwId"
	ResolvedViaUnresolved  = "unresolved"
)

// resolvePoint implements the boomId -> boomNieuwId -> unresolved lookup order (design.md D3):
// given one kapenherplant row's boomId/boomNieuwId and the set of stamgegevens ids known to exist,
// it returns which stamgegevens id (if any) resolves the row's point, and via which key. It never
// errors and never signals "skip" — an unresolved row is a first-class outcome
// (specs/bomen-load/spec.md), never a failure.
//
// Pure/no DB, so it is unit-testable directly over fixture maps; join.go's resolveJoin is the
// DB-executing pass that calls this once per kapenherplant row and writes the result back.
func resolvePoint(boomID, boomNieuwID string, stamgegevensIDs map[string]struct{}) (resolvedID, via string) {
	if boomID != "" {
		if _, ok := stamgegevensIDs[boomID]; ok {
			return boomID, ResolvedViaBoomID
		}
	}
	if boomNieuwID != "" {
		if _, ok := stamgegevensIDs[boomNieuwID]; ok {
			return boomNieuwID, ResolvedViaBoomNieuwID
		}
	}
	return "", ResolvedViaUnresolved
}
