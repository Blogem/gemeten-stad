// Package places projects the PostGIS gebieden backbone (gebieden_buurten, gebieden_wijken) into
// a gs:Place Turtle candidate for the SHACL-gated graph load: identity, rdfs:label, buurt->wijk
// gs:within containment, and an evolving gs:active status (RDF-star gs:validFrom annotation). The
// read (thin, schema-qualified) is split from the render (pure, DB-free) so the projection logic
// is unit-testable without a live store — see openspec/changes/seed-place-skeleton/design.md D2.
package places
