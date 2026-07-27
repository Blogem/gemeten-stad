// Package koop maps landed KOOP permit publications into the conformed model.
//
// It reads the raw corpus load/koop lands under the shared raw store (one SRU record per
// publication, paired with an optional metadata sidecar carrying its zaaknummer), dedups each zaak
// down to the single besluit — or, absent one yet, its ontwerpbesluit stand-in — worth auditing,
// resolves that besluit's location and scopes it to stadsdeel Noord, then idempotently persists two
// things:
//
//   - the audited besluit as an Intervention/Claim/Place triple, written through the graph's
//     SHACL-gated load path (load/graph.Load) — a non-conforming candidate writes nothing;
//   - the full publication trail — every publication in a zaak, resolved or not, plus keyless
//     publications with no zaaknummer at all — into PostGIS, staged and upserted by change.
//
// See Load (load.go) for the orchestration and its scoping policy: which zaken get graphed,
// excluded, or kept pending.
package koop
