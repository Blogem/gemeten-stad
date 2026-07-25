#!/usr/bin/env python3
"""Spike C probe 1 — per-activity count parser vs the naive largest-int baseline.

Can we extract the felled/moved tree counts from the permit prose, and does
splitting the count *per activity* matter? Re-parses every kap-permit `abstract`
(reused from ../spike-b, no re-harvest) into a list of (activity, count) pairs
instead of Spike B's single number.

The abstract is short, formulaic prose — "het <verb> van <N> bomen (en <verb2>
van <M> boom) …" — so a deterministic pattern parser, not NER, is the right tool.
The parser: splits multi-activity clauses so *vellen* and *verplanten* are never
summed; binds a count that may sit before OR after its verb; reads spelled-out
Dutch numbers (een…twintig) the `\\d+` regex misses; keeps the *herplant/plant*
(replant) count strictly on the replant side, never in the felling total; and
flags the countless buckets ("diverse/meerdere/een aantal bomen").

Baseline = Spike B's `parse_count` (harvest_permits.py:57): the LARGEST integer
next to "bomen" — which silently conflates vellen+verplant (picks 43 from "vellen
van 33 … verplanten van 43") and can grab a herplant promise as the felling count.

Reports yield vs baseline, the conflation rate, the spelled-out lift, and the
clause-count distribution — split by doctype (besluit/aanvraag) and scope
(citywide/Noord). Stdlib only; read-only (reads a local file). Run from this dir:
  python3 parse_activities.py
Reads ../spike-b/all_permits.jsonl. Writes activities.jsonl.
"""
import collections
import json
import re

SRC = "../spike-b/all_permits.jsonl"
OUT = "activities.jsonl"

# Activity verb stems → normalized activity. Order matters: match verplant/herplant
# before the bare "plant"/"kap" stems so the prefix wins.
VERB_STEMS = [
    ("verplant", "verplanten"),
    ("herplant", "herplanten"),
    ("rooi", "rooien"),
    ("vell", "vellen"),
    ("kap", "kappen"),
    ("plant", "herplanten"),   # bare "planten" = replant side
]
FELLING = {"kappen", "vellen", "rooien", "verplanten"}  # obligation (art. 1k: verplant IS vellen)
REPLANT = {"herplanten"}                                 # fulfilment side, never a felling count

WORD_NUM = {
    "een": 1, "één": 1, "twee": 2, "drie": 3, "vier": 4, "vijf": 5, "zes": 6,
    "zeven": 7, "acht": 8, "negen": 9, "tien": 10, "elf": 11, "twaalf": 12,
    "dertien": 13, "veertien": 14, "vijftien": 15, "zestien": 16, "zeventien": 17,
    "achttien": 18, "negentien": 19, "twintig": 20,
}
COUNTLESS = ("diverse", "meerdere", "een aantal", "enkele", "verschillende", "aantal")
NOUN = r"(?:bo{1,2}m|bomen|houtopstand(?:en)?)"   # boom/bomen/houtopstand(en)


def naive_count(text):
    """Spike B baseline: the largest integer written next to bo(o)m/bomen."""
    if not text:
        return None
    nums = [int(m.group(1)) for m in re.finditer(r"(\d+)\s*(?:bo{1,2}m|bomen)", text, re.I)]
    return max(nums) if nums else None


def verb_of(word):
    w = word.lower()
    for stem, act in VERB_STEMS:
        if w.startswith(stem):
            return act
    return None


def a_number(token):
    """(value, spelled_out) for a token that is a digit or a Dutch number word."""
    t = token.lower().strip(".,()")
    if t.isdigit():
        return int(t), False
    if t in WORD_NUM:
        return WORD_NUM[t], True
    return None, False


def clause_count(clause):
    """Count nearest the NOUN in a clause: (value, spelled_out, countless)."""
    low = clause.lower()
    for phrase in COUNTLESS:
        if re.search(phrase + r"\s+" + NOUN, low):
            return None, False, True
    # a number token immediately before the noun ("33 bomen", "een boom", "vier bomen",
    # "7 houtopstanden" — the noun the naive baseline misses). Only NOUN-adjacent numbers
    # count: a bare digit elsewhere is a house number / coordinate / OLO ref, not a tree count.
    m = re.search(r"(\S+)\s+" + NOUN, low)
    if m:
        val, spelled = a_number(m.group(1))
        if val is not None:
            return val, spelled, False
    return None, False, False


def parse_abstract(text):
    """List of {verb,count,spelled_out,bucket} — one entry per activity clause."""
    if not text:
        return []
    # normalize; strip the trailing OLO/reference noise that carries stray digits
    text = re.sub(r"\bOLO\s*\d+", " ", text, flags=re.I)
    text = re.sub(r"boomnummer\s*\d+", " ", text, flags=re.I)
    text = re.sub(r"\s+", " ", text).strip()
    clauses = re.split(r"\s+en\s+|[,;]", text)
    out = []
    last_verb = None
    for cl in clauses:
        cl = cl.strip()
        if not cl:
            continue
        verbs = [verb_of(w) for w in re.findall(r"[A-Za-zéï'-]+", cl)]
        verbs = [v for v in verbs if v]
        val, spelled, countless = clause_count(cl)
        verb = verbs[0] if verbs else last_verb   # a bare count-clause continues prior verb
        if verb is None:
            continue
        if verbs:
            last_verb = verbs[0]
        if val is None and not countless:
            continue                               # a verb with no count states nothing to size
        out.append({"verb": verb, "count": val, "spelled_out": spelled,
                    "bucket": "countless" if countless else "counted"})
    return out


def felling_total(acts):
    vals = [a["count"] for a in acts if a["verb"] in FELLING and a["count"] is not None]
    return sum(vals) if vals else None


def report(rows):
    def block(label, subset):
        n = len(subset)
        if not n:
            return
        parsed = [r for r in subset if r["activities"]]
        base = [r for r in subset if r["naive_count"] is not None]
        yielded = [r for r in subset if felling_total(r["activities"]) is not None]
        multi = [r for r in subset if len({a["verb"] for a in r["activities"]}) >= 2]
        conflated = [r for r in subset if r["conflated"]]
        lift = [r for r in subset if r["naive_count"] is None
                and felling_total(r["activities"]) is not None]
        countless = [r for r in subset if any(a["bucket"] == "countless" for a in r["activities"])]
        print(f"\n=== {label} (n={n}) ===")
        print(f"  naive baseline count present:      {len(base):4d}/{n} ({100*len(base)/n:3.0f}%)")
        print(f"  parser: a felling total extracted: {len(yielded):4d}/{n} ({100*len(yielded)/n:3.0f}%)")
        print(f"  parser lifts a count naive missed: {len(lift):4d}  (spelled-out / spacing)")
        print(f"  multi-activity abstracts:          {len(multi):4d}  (vellen+verplant etc.)")
        print(f"  naive CONFLATED activities:        {len(conflated):4d}  "
              f"(largest-int != felling total)")
        print(f"  countless bucket (diverse/…):      {len(countless):4d}")
        dist = collections.Counter(len({a['verb'] for a in r['activities']})
                                   for r in parsed)
        print(f"  activities/abstract distribution:  {dict(sorted(dist.items()))}")

    print(f"corpus: {len(rows)} abstracts")
    for scope_label, scope in [("CITYWIDE", rows), ("NOORD", [r for r in rows if r["noord"]])]:
        for dt in ("besluit", "aanvraag"):
            block(f"{scope_label} · {dt}", [r for r in scope if r["doctype"] == dt])


def main():
    permits = [json.loads(l) for l in open(SRC)]
    rows = []
    for p in permits:
        acts = parse_abstract(p.get("abstract"))
        nc = naive_count(f'{p.get("title")} {p.get("abstract")}')
        ft = felling_total(acts)
        # naive is wrong when its single largest-int != the obligation sum: it either grabbed a
        # herplant (replant) number as the felling count, or returned max() instead of the sum
        # of the felling activities ("vellen 33 en verplanten 43" → obligation 76, naive 43).
        conflated = bool(acts and nc is not None and ft is not None and nc != ft)
        rows.append({"id": p["id"], "doctype": p["doctype"], "noord": p.get("noord", False),
                     "abstract": p.get("abstract"), "activities": acts,
                     "naive_count": nc, "felling_total": ft, "conflated": conflated})
    with open(OUT, "w") as f:
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    report(rows)
    print(f"\nwrote {OUT} ({len(rows)} rows)")


if __name__ == "__main__":
    main()
