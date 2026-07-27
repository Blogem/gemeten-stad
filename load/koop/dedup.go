package koop

// groupByZaak groups pubs by Zaaknummer. Keyless publications (Zaaknummer == "", i.e. no landed
// metadata sidecar) are never audited via selectBesluit, but they ARE kept in the PostGIS trail,
// so they are grouped too, under the "" key, rather than dropped — callers that only care about
// auditable zaken simply skip that bucket.
func groupByZaak(pubs []Publication) map[string][]Publication {
	byZaak := make(map[string][]Publication)
	for _, pub := range pubs {
		byZaak[pub.Zaaknummer] = append(byZaak[pub.Zaaknummer], pub)
	}
	return byZaak
}

// selectBesluit picks the single publication, from one zaak's publications, that gets audited:
//
//   - Candidates are every publication with Kind == KindBesluit.
//   - If there are none (the zaak hasn't reached a final besluit yet), fall back to every
//     publication with Kind == KindOntwerpbesluit instead.
//   - Among the candidate set, pick the one with the latest Available date (plain string compare
//     is safe: dates are landed as "YYYY-MM-DD", which sorts lexicographically the same as
//     chronologically).
//   - ok is false when neither a besluit nor an ontwerpbesluit exists — the zaak is still pending
//     (only an aanvraag, or withdrawn) and has nothing yet worth auditing.
//
// Rationale: a besluit is the government's final word on a zaak and is what gets checked against
// observation data; an ontwerpbesluit is used only as a stand-in when no final besluit has landed
// yet, since it is the best available signal of what the eventual decision will look like.
func selectBesluit(pubs []Publication) (besluit Publication, ok bool) {
	candidates := filterByKind(pubs, KindBesluit)
	if len(candidates) == 0 {
		candidates = filterByKind(pubs, KindOntwerpbesluit)
	}
	if len(candidates) == 0 {
		return Publication{}, false
	}

	latest := candidates[0]
	for _, c := range candidates[1:] {
		if c.Available > latest.Available {
			latest = c
		}
	}
	return latest, true
}

// filterByKind returns every publication in pubs whose Kind matches kind.
func filterByKind(pubs []Publication, kind Kind) []Publication {
	var matches []Publication
	for _, p := range pubs {
		if p.Kind == kind {
			matches = append(matches, p)
		}
	}
	return matches
}
