## ADDED Requirements

### Requirement: SCD2 versioning of node-form period nodes grouped by a series anchor

The idempotent SCD2 upsert SHALL version time-bounded state as **node-form period nodes**: a node
carrying a plain-triple `gs:validFrom` **and** a `gs:versionOf <anchor>` pointer is a **period** in
the series identified by the stable `<anchor>` IRI. The writer SHALL maintain **exactly one open**
(no `gs:validTo`) period per anchor, with the same guarantees the RDF-star annotation form already
provides:

- **Idempotency by content-derived identity.** Period nodes have caller-assigned IRIs that are stable
  for unchanged outcome content and different for changed content. A period whose valid-time-agnostic
  content matches an already-present period (ignoring `gs:validFrom`/`gs:validTo`) SHALL be treated as
  **unchanged** — a true no-op that retains the stored period's `gs:validFrom`.
- **Open/close on a new period.** When a **new** period node appears for an anchor (a new IRI carrying
  `gs:versionOf <anchor>` and `gs:validFrom`), the writer SHALL **close** the prior open period(s) of
  that same anchor by stamping their `gs:validTo` equal to the new period's `gs:validFrom` (contiguous
  intervals), SHALL NOT overwrite or delete them (history retained), and SHALL leave exactly one open
  period per anchor.
- **Invariant across both forms.** The open-version invariant SHALL hold whether an entity is
  expressed as a node-form period (one open per `gs:versionOf` anchor) or an RDF-star annotation (one
  open per subject); a candidate that would leave more than one open version SHALL fail loudly with
  nothing written.
- The writer SHALL NOT invent `gs:validFrom` (it reads the new period's stamp to use as the prior's
  `gs:validTo`) and SHALL remain free of any value-store access.

#### Scenario: A new period closes the prior and opens

- **WHEN** an anchor already has one open period, and a load carries a **new** period node for that
  anchor (new content-derived IRI, `gs:versionOf <anchor>`, `gs:validFrom t2`)
- **THEN** the prior open period is closed with `gs:validTo = t2`, the new period is written open, and
  exactly one open period remains for that anchor
- **AND** the prior period's triples remain in the store (history is not overwritten)

#### Scenario: An unchanged period series re-run is a no-op

- **WHEN** a load re-asserts a period whose outcome content is unchanged (only a fresh `gs:validFrom`
  would differ), producing the same content-derived IRI
- **THEN** no new period is written, no prior is closed, and no `run:load-…` graph is minted

#### Scenario: The open-period invariant is enforced per anchor

- **WHEN** a load would result in two open periods for one `gs:versionOf` anchor
- **THEN** the writer fails loudly and writes nothing (no closed priors, no run graph, no
  `prov:Activity`)
