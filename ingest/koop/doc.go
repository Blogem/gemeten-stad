// Package koop harvests kap/verplant omgevingsvergunningen from KOOP (scoped SRU query).
//
// For each publication it lands two verbatim artifacts under the koop/ prefix, keyed by the same
// gmb identifier: the SRU gzd record (koop/<id>.xml) and its metadata.xml sidecar
// (koop/<id>.metadata.xml). The sidecar is the authoritative — and only landed — source of the
// zaaknummer (OVERHEIDop.referentienummer, whose prefix encodes the stadsdeel), which is absent
// from the SRU record; it also carries OVERHEIDop.activiteit and OVERHEIDop.gebiedsmarkering. Both
// artifacts are landed as-is (no field extraction at harvest — that is the load stage's job); the
// sidecar fetch is gated on need (skipped when already landed and the record is unchanged) and a
// missing sidecar is non-fatal.
//
// The sidecar host throttles aggressively under load, so fetches are paced
// (GS_KOOP_METADATA_RATE_MS, default 200ms) and retried with capped backoff; a circuit breaker
// aborts the harvest after GS_KOOP_METADATA_MAX_CONSECUTIVE_FAILS (default 2) consecutive fetches
// exhaust their retries — the signature of an active block. The harvest is resumable: landed
// sidecars are kept and the cursor is not advanced on an abort, so re-running picks up the rest.
package koop
