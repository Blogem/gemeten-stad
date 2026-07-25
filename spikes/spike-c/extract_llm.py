#!/usr/bin/env python3
"""Spike C probe 5b — LLM count binding (the msr-graph binding choice), for comparison.

The spaCy probe (5a) showed recognition is solved by the vocab but the *binding* residue —
species appositive ("een boom, de Es" = one tree, not two), "1 of 2 bomen", multi-activity in
one clause, snoeien vs vellen — is where a proximity/dep-parse binder errs. `msr-graph` binds
quantities with an LLM over a sentence window + closed-set validation; this probe does the same
so we can measure whether the LLM dependency **significantly** beats the deterministic binders.

The LLM reads the one-sentence abstract and returns per-activity counts against a CLOSED
activity set, with the same rules the audit uses (verplant = felling; herplant/snoeien ≠
obligation; a boom and its species/appositive are one tree). Runs only over the comparison
sample (disagreements ∪ deterministic residual ∪ an easy baseline) to keep it cheap.

Reads DEEPSEEK_API_KEY (+ optional DEEPSEEK_BASE_URL, default https://api.deepseek.com) from the
ENVIRONMENT — never from any file. Run from this dir after probes 1 & 5a:
  set -a; source ~/code/msr-graph/.env; set +a       # or export DEEPSEEK_API_KEY=...
  . .venv/bin/activate && python extract_llm.py
Reads activities.jsonl, ner_counts.jsonl, ../spike-b/all_permits.jsonl. Writes llm_counts.jsonl.
"""
import concurrent.futures as cf
import json
import os
import sys
import urllib.request

API_KEY = os.environ.get("DEEPSEEK_API_KEY")
BASE = os.environ.get("DEEPSEEK_BASE_URL", "https://api.deepseek.com").rstrip("/")
MODEL = os.environ.get("LLM_MODEL_EXTRACT") or os.environ.get("DEEPSEEK_MODEL", "deepseek-chat")
OUT = "llm_counts.jsonl"

SYSTEM = (
    "Je extraheert boom-aantallen uit Amsterdamse kap-vergunningteksten. Geef JSON: "
    '{"activities":[{"activity":"<vellen|verplanten|herplanten|snoeien>","count":<int|null>}]}. '
    "Regels: (1) 'kappen','vellen','rooien' en 'verplanten' zijn allemaal VELLEN (verplanten telt "
    "mee als kap). (2) 'herplanten'/'planten' = herplanten (fulfilment, GEEN kap). (3) 'snoeien' = "
    "snoeien (boom blijft, GEEN kap). (4) Eén boom met zijn soortnaam of tussenzin is ÉÉN boom "
    "('een boom, de Es' = 1; 'een boom (rode beuk)' = 1). (5) Getallen mogen voluit geschreven zijn "
    "('vierenveertig'=44). (6) Enkelvoud zonder getal = 1 ('Kappen Ceder'=1). (7) Negeer huisnummers, "
    "boomnummers en OLO-nummers. (8) '1 of 2 bomen' -> count 2 (bovengrens). Alleen JSON, geen uitleg."
)


def call(abstract):
    body = json.dumps({
        "model": MODEL,
        "messages": [{"role": "system", "content": SYSTEM},
                     {"role": "user", "content": abstract or ""}],
        "temperature": 0, "response_format": {"type": "json_object"},
    }).encode()
    req = urllib.request.Request(f"{BASE}/chat/completions", data=body, headers={
        "Authorization": f"Bearer {API_KEY}", "Content-Type": "application/json"})
    for _ in range(3):
        try:
            with urllib.request.urlopen(req, timeout=90) as r:
                content = json.load(r)["choices"][0]["message"]["content"]
            return json.loads(content).get("activities", [])
        except Exception:  # noqa: BLE001 — transient / malformed; retry then give up
            pass
    return None


def obligation(items):
    if items is None:
        return None
    vals = [i["count"] for i in items
            if i.get("activity") in ("vellen", "verplanten") and isinstance(i.get("count"), int)]
    return sum(vals) if vals else None


def sample_ids():
    """The comparison set: det↔spaCy disagreements ∪ det residual ∪ an easy baseline."""
    det = {json.loads(l)["id"]: json.loads(l) for l in open("activities.jsonl")}
    ner = {json.loads(l)["id"]: json.loads(l) for l in open("ner_counts.jsonl")}
    disagree, resid, easy = [], [], []
    for i, d in det.items():
        n = ner.get(i, {})
        df, nf = d["felling_total"], n.get("ner_obligation")
        is_fell = any(a["verb"] in ("kappen", "vellen", "rooien", "verplanten") for a in d["activities"]) \
            or (d["abstract"] and any(w in d["abstract"].lower() for w in ("kap", "vell", "rooi", "verplant")))
        if not is_fell:
            continue
        if df is not None and nf is not None and df != nf:
            disagree.append(i)
        elif df is None:
            resid.append(i)
        elif len(easy) < 100:
            easy.append(i)
    return disagree, resid[:120], easy


def main():
    if not API_KEY:
        sys.exit("DEEPSEEK_API_KEY not set in the environment — see the module docstring.")
    abstext = {json.loads(l)["id"]: json.loads(l).get("abstract")
               for l in open("../spike-b/all_permits.jsonl")}
    disagree, resid, easy = sample_ids()
    ids = list(dict.fromkeys(disagree + resid + easy))
    print(f"LLM sample: {len(ids)} abstracts "
          f"(disagree {len(disagree)} · residual {len(resid)} · easy {len(easy)})",
          file=sys.stderr)
    rows = {}
    with cf.ThreadPoolExecutor(max_workers=8) as ex:
        futs = {ex.submit(call, abstext.get(i)): i for i in ids}
        for k, fut in enumerate(cf.as_completed(futs), 1):
            i = futs[fut]
            items = fut.result()
            rows[i] = {"id": i, "abstract": abstext.get(i), "llm_items": items,
                       "llm_obligation": obligation(items)}
            if k % 25 == 0:
                print(f"\r  {k}/{len(ids)}", end="", file=sys.stderr, flush=True)
    print(file=sys.stderr)
    with open(OUT, "w") as f:
        for i in ids:
            f.write(json.dumps(rows[i], ensure_ascii=False) + "\n")
    got = sum(1 for r in rows.values() if r["llm_obligation"] is not None)
    print(f"LLM returned an obligation count on {got}/{len(ids)} sampled abstracts")
    print(f"wrote {OUT}")


if __name__ == "__main__":
    main()
