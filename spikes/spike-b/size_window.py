#!/usr/bin/env python3
"""Size the Noord kap corpus per year 2020..2026 by reusing spike-b's harvest logic.

Run from spikes/spike-b/ (needs ../spike-a/noord_buurt_ids.txt + buurt_cache.json).
Prints, per year: citywide publications, Noord publications, Noord besluiten.
"""
import importlib.util
import sys
import time
import urllib.request

# The SRU endpoint drops the default Python-urllib UA -> install a browser UA globally.
_op = urllib.request.build_opener()
_op.addheaders = [("User-Agent", "Mozilla/5.0")]
urllib.request.install_opener(_op)

spec = importlib.util.spec_from_file_location("hp", "harvest_permits.py")
hp = importlib.util.module_from_spec(spec)
spec.loader.exec_module(hp)

# The SRU endpoint is flaky (SSL EOF) and hp.sru_page has no retry -> wrap it.
_raw_sru = hp.sru_page
def _sru_retry(start, n, _tries=8):
    for i in range(_tries):
        try:
            return _raw_sru(start, n)
        except Exception:  # noqa: BLE001 — transient TLS/SRU errors; retry w/ backoff
            if i == _tries - 1:
                raise
            time.sleep(2 * (i + 1))
hp.sru_page = _sru_retry

YEARS = [int(a) for a in sys.argv[1:]] or list(range(2020, 2027))
rows = []
for y in YEARS:
    hp.YEAR = y
    hp.QUERY = ('(dt.creator any "Amsterdam" AND dt.type any "omgevingsvergunning" '
                'AND cql.textAndIndexes any "kappen vellen rooien verplanten houtopstand" '
                f'AND dt.available>="{y}-01-01" AND dt.available<="{y}-12-31")')
    permits = list(hp.harvest())
    permits = hp.resolve_noord(permits)
    noord = [p for p in permits if p.get("noord")]
    besl = [p for p in noord if p["doctype"] == "besluit"]
    rows.append((y, len(permits), len(noord), len(besl)))
    print(f"  -> {y}: city={len(permits)} noord={len(noord)} besluiten={len(besl)}",
          file=sys.stderr)

print("\n=== Noord kap corpus per year ===")
print(f"{'year':>6} {'city pubs':>10} {'noord pubs':>11} {'noord besluiten':>16}")
for y, c, n, b in rows:
    print(f"{y:>6} {c:>10} {n:>11} {b:>16}")
print(f"{'TOTAL':>6} {sum(r[1] for r in rows):>10} {sum(r[2] for r in rows):>11} "
      f"{sum(r[3] for r in rows):>16}")
