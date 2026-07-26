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
package koop
