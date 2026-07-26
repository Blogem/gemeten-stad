## 1. Per-source ingest selection (`cmd/pipeline`)

- [x] 1.1 Add an ordered source registry (`name → func(ctx, *shared.RawStore) error`) in `cmd/pipeline` with `bag` and `gebieden` entries, wiring the existing `bag.Ingest`/`gebieden.Ingest` calls (BAG first, then gebieden/CBS).
- [x] 1.2 Add a pure resolver `selectSources(args []string) (names []string, err error)`: empty args → all registered names in registration order; named args → those names in the given order; an unknown name → error listing the valid names.
- [x] 1.3 Rewire the `ingest` subcommand to take positional source args (`pipeline ingest [source ...]`), resolve them via `selectSources`, build the raw store once, and run the selected ingesters in order; a resolve error exits non-zero before any ingest runs.

## 2. Docs

- [x] 2.1 Add the per-source invocation form to `deploy/compose/README.md` §1 (`pipeline ingest bag` monthly, `pipeline ingest gebieden` weekly; no args = all).

## 3. Tests

All Go tests use **testify** (`require`/`assert`), table-driven where it fits.

- [x] 3.1 Unit-test `selectSources` (pure, no network/DB): no args → all names in registration order; a single valid name → just that name; multiple names → those in order; an unknown name → error whose message lists the valid names; an unknown mixed with valid names → error (nothing selected).
- [x] 3.2 Unit-test the `ingest` command wiring: `pipeline ingest <unknown>` exits non-zero with the valid-names error and invokes no ingester; the registry contains exactly `bag` and `gebieden`. Do not execute the real network ingesters in a unit test.
