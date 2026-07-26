## ADDED Requirements

### Requirement: SKOS ConceptScheme seeded from authoritative sources

`ontology/vocab.ttl` SHALL define a SKOS `ConceptScheme` for the tree-audit domain whose
concepts are seeded from authoritative sources — not invented — covering the slices the
vertical actually touches: the legal top from the regulation (houtopstand, herplantplicht,
herplantfonds, monumentale boom, and the diameter/stamomtrek classes), tree species, activity
terms, tree-defect terms, and status vocabularies. Each concept SHALL have a `skos:prefLabel`
(Dutch) and, where an authoritative source concept exists (TOOI, IMBOR/BOOM,
Nederlands Soortenregister), a `skos:exactMatch` to that source IRI rather than a re-minted
parallel identifier. Concept identity SHALL rest on codes/IRIs, never on names.

#### Scenario: Vocab loads and aligns to sources

- **WHEN** `ontology/vocab.ttl` is loaded into Fuseki
- **THEN** it loads without error as valid SKOS
- **AND** the legal-top concepts (herplantplicht, herplantfonds, houtopstand, monumentale boom)
  are present with `skos:prefLabel`
- **AND** concepts backed by an authoritative source carry a `skos:exactMatch` to that source
  IRI

#### Scenario: Corpus-mined labels are not in v0

- **WHEN** the v0 vocab is inspected
- **THEN** its labels derive only from authoritative sources and data enums (corpus-mined
  altLabels are deferred to the Phase-2 feedback loop)

### Requirement: Activity vocabulary treats verplanten as vellen

The activity vocabulary SHALL model felling as a single concept (from the
`kapenherplant.boommaatregelBesluit` enum's one meaningful value, `Vellen (boom verwijderen)`)
carrying `verplanten`, `rooien`, `kappen`, and `kandelaberen` as `skos:altLabel`s, with a
`transplantOrigin` caveat term registered for transplants. There SHALL NOT be a separate
`Verplanten` concept — per Bomenverordening 2014 Art. 1 and Spike C, a transplant is legally a
velling.

#### Scenario: Verplanten resolves to the felling concept

- **WHEN** the surface form "verplanten" (or "rooien", "kappen") is looked up in the vocab
- **THEN** it resolves as an `skos:altLabel` of the single felling concept
- **AND** no distinct `Verplanten` concept exists

### Requirement: Species concepts seeded city-wide

The vocab SHALL seed a species concept for every distinct `soortnaam` value in the loaded
city-wide `stamgegevens` Postgres table (which `load/bomen` populates for all of Amsterdam),
not only the Noord vertical's corpus — each with its singular `skos:prefLabel` and its source
value preserved as identity. The species list SHALL be read from that loaded table (a
`SELECT DISTINCT soortnaam`), not re-fetched from the Datapunt API. Seeding species city-wide up
front is deliberate — the set is small and bounded, and it removes a scale-up gap.

#### Scenario: City-wide species present

- **WHEN** the vocab's species concepts are inspected
- **THEN** they cover the distinct `soortnaam` values in the loaded city-wide `stamgegevens`
  table, not just the species attested in the Noord corpus

### Requirement: Species concepts carry plural and inflected surface forms

Every species concept whose `prefLabel` is a Dutch common noun SHALL carry its grammatical
plural/inflected Dutch surface form(s) and its Latin genus as `skos:altLabel`s (e.g. iep/iepen,
es/essen, populier/populieren, els/elzen). These plurals SHALL be authored for the whole
city-wide species set in v0 — not only the common species — and SHALL be verified against an
open Dutch lexicon (OpenTaal or nl.wiktionary `meervoud`) where an entry exists; the common
species that appear in permit prose SHALL use Spike C's corpus-attested forms
(`spikes/spike-c/vocab.py`). A species concept whose identifier is a Latin binomial or a
non-lexical string MAY carry only its Latin/source form (no invented plural). This is
load-bearing: the spaCy EntityRuler is seeded from these labels and the `nl_core_news_md`
lemmatizer mis-normalizes botanical plurals (Spike C), so a singular-only vocab silently fails
to recognize species-headed counts. (Corpus-mined *surface variants* — abbreviations,
misspellings, cultivar phrasings — are distinct from grammatical plurals and remain a Phase-2
concern.)

#### Scenario: Every Dutch-common-noun species has a plural altLabel

- **WHEN** the vocab's species concepts are inspected
- **THEN** each species whose `prefLabel` is a Dutch common noun has at least one
  plural/inflected `skos:altLabel` distinct from its singular `skos:prefLabel`
- **AND** its Latin genus is present as an `skos:altLabel`

#### Scenario: Plural surface form resolves to its species concept

- **WHEN** the surface form "essen" (plural of es) is looked up
- **THEN** it resolves to the same concept as "es"

### Requirement: Data-enum concepts ingested; places live in the value store

The vocab SHALL ingest the distinct values of the touched data enums that a consumer uses — the
`kapenherplant.boomAanwezigheid` status axis (the fulfilment signal, Spike C) — as SKOS concepts
with their source value preserved as identity, so the `gs:status` controlled-value shape can bind
to them. (Species are handled by the city-wide species requirement above.)

Places (gebieden buurten/wijken, CBS buurten) SHALL NOT be seeded as SKOS concepts. A place's
meaning is its geometry and its buurt→wijk→stadsdeel membership, which are authoritative in the
PostGIS value store (`gebieden_buurten`: naam + code + geom + `ligtinwijkid`); a SKOS name→code
copy would add nothing the value store lacks, invite drift, and still force any real place query
(containment, aggregation) back to PostGIS. Places SHALL instead live as `gs:Place` instance nodes
keyed by their gebieden/BAG code, resolving to the value store, and the analytics agent SHALL be
given a value-store query capability for locations (a Phase-2 server/agent concern), not a vocab
shadow. `boomgebreken` is likewise not ingested here — it is the separate un-ingested
`gebrekregistratie` dataset (a `load/bomen`/P7 scope addition), deferred and recorded in the vocab
header.

#### Scenario: Status enum values become concepts with their source value

- **WHEN** a `boomAanwezigheid` value is represented in the vocab
- **THEN** it is a SKOS concept whose identity carries the source value (a `skos:notation`), and
  the `gs:status` shape resolves against it

#### Scenario: Places are not vocab concepts

- **WHEN** the v0 vocab is inspected for gebieden/CBS places
- **THEN** no place is a SKOS concept; places are represented only as `gs:Place` instance nodes
  keyed by code, resolving to the PostGIS value store (which holds their names, geometry, and
  hierarchy)

### Requirement: Whole-city and multi-intervention scope is a planned Phase-4 revisit

The vocabulary SHALL document that v0's concept coverage (including grammatical plurals) is
Amsterdam-wide, and that what remains deferred is: corpus-mined *surface variants* (Phase-2
mining, which runs against the Noord backfill corpus first), and the vocabulary slices for a
second intervention type (Phase 4 — the seam test; IMPLEMENTATION_PLAN §6). No requirement here
SHALL assume Noord-only coverage in a way that silently breaks at city scale.

#### Scenario: Revisit is recorded, not implicit

- **WHEN** the vocab artifact (or its accompanying doc) is reviewed
- **THEN** it states that corpus-mined surface variants (Phase 2) and additional
  intervention-type slices (Phase 4) are planned revisits, so the scale-up is not a surprise
