#!/usr/bin/env python3
"""Spike C probe 5a — vocabulary-driven spaCy extraction (the msr-graph pattern).

Instead of hand-growing the deterministic parser's rules (probe 1) to chase the residual,
recognize the tree terms from the **vocabulary** and let the parser bind the counts — the
`msr-graph` architecture: `spacy` pipeline + an **EntityRuler seeded from the graph vocab**
(here `vocab.py`, a stand-in for the SKOS SPARQL read), each pattern carrying its concept IRI
in `id` → resolved via `ent_id_`. Species that the deterministic NOUN regex missed ("essen",
"populieren", "een iep", "Kappen Ceder") are now recognized because the vocab supplies the
surface forms; counts bind to them via the dependency parse (`nummod`/`det`), with a Dutch
number normalizer for spelled-out + compound forms and implicit-1 for a bare singular.

Measures what this recovers over the deterministic parser's **residual** (felling permits it
left with no count), and agreement where both fire. Compares against `activities.jsonl`.

Needs the local venv (`spacy` + `nl_core_news_md`). Run from this dir (after probe 1):
  . .venv/bin/activate && python extract_ner.py
Reads ../spike-b/all_permits.jsonl, activities.jsonl, vocab.py. Writes ner_counts.jsonl.
"""
import json
import re

import spacy

from vocab import patterns

SRC = "../spike-b/all_permits.jsonl"
DET = "activities.jsonl"
OUT = "ner_counts.jsonl"

UNITS = {"een": 1, "één": 1, "twee": 2, "drie": 3, "vier": 4, "vijf": 5, "zes": 6,
         "zeven": 7, "acht": 8, "negen": 9}
TEENS = {"tien": 10, "elf": 11, "twaalf": 12, "dertien": 13, "veertien": 14, "vijftien": 15,
         "zestien": 16, "zeventien": 17, "achttien": 18, "negentien": 19}
TENS = {"twintig": 20, "dertig": 30, "veertig": 40, "vijftig": 50, "zestig": 60,
        "zeventig": 70, "tachtig": 80, "negentig": 90}
_COMPOUND = re.compile(r"^(een|één|twee|drie|vier|vijf|zes|zeven|acht|negen)(?:en|ën)"
                       r"(twintig|dertig|veertig|vijftig|zestig|zeventig|tachtig|negentig)$")


def parse_nl_number(text):
    """Dutch number word / digit → int, incl. compounds ('vierenveertig'=44)."""
    t = text.lower().strip(".,()'\"")
    if t.isdigit():
        return int(t)
    if t in UNITS: return UNITS[t]
    if t in TEENS: return TEENS[t]
    if t in TENS: return TENS[t]
    m = _COMPOUND.match(t)
    if m:
        return UNITS[m.group(1)] + TENS[m.group(2)]
    return None


def build_nlp():
    nlp = spacy.load("nl_core_news_md", disable=["ner"])   # keep parser/tagger; drop stat-NER
    pats, meta = patterns()
    ruler = nlp.add_pipe("entity_ruler", config={"phrase_matcher_attr": "LOWER"})
    ruler.add_patterns(pats)
    return nlp, meta


def count_for(root):
    """Bind a count to a tree-noun token via the dependency parse, else a left window."""
    for child in root.children:                       # nummod ('22 bomen') or det ('een boom')
        if child.dep_ in ("nummod", "det") or child.pos_ == "NUM":
            v = parse_nl_number(child.text)
            if v is not None:
                return v
    for j in range(root.i - 1, max(-1, root.i - 4), -1):   # fallback: nearest number to the left
        v = parse_nl_number(root.doc[j].text)
        if v is not None:
            return v
    return None


def extract(doc, meta):
    """Per-tree-noun (activity_type, count) using vocab entities + the parse."""
    acts = [(e.start, meta[e.ent_id_][1]) for e in doc.ents if meta[e.ent_id_][0] == "ACTIVITY"]
    trees = [(e.start, e.end, e.root, meta[e.ent_id_][1]) for e in doc.ents
             if meta[e.ent_id_][0] == "TREE"]
    trees.sort()
    # merge adjacent tree ents ("kastanje boom", "Rode Paardenkastanjes") into one tree
    merged = []
    for start, end, root, hint in trees:
        if merged and start <= merged[-1][1] + 1:
            merged[-1] = (merged[-1][0], end, root, "pl" if "pl" in (merged[-1][3], hint) else hint)
        else:
            merged.append((start, end, root, hint))
    out = []
    for start, end, root, hint in merged:
        n = count_for(root)
        if n is None and hint == "sg":
            n = 1                                     # implicit-1: a bare singular tree
        if n is None:
            continue                                  # plural with no number → genuinely unknown
        atype = next((t for s, t in sorted(acts, reverse=True) if s < start),  # nearest left
                     acts[0][1] if acts else "felling")
        out.append({"activity": atype, "count": n})
    return out


def obligation(items):
    vals = [i["count"] for i in items if i["activity"] == "felling"]
    return sum(vals) if vals else None


def main():
    nlp, meta = build_nlp()
    permits = [json.loads(l) for l in open(SRC)]
    det = {json.loads(l)["id"]: json.loads(l) for l in open(DET)}
    rows, recov, agree, disagree = [], 0, 0, 0
    resid = ner_resid_hit = 0
    for p in permits:
        text = p.get("abstract") or ""
        # strip reference noise carrying stray digits: OLO refs, boomnummers, and parenthetical
        # id numbers "(12)" — but keep species parentheticals like "(iep)".
        text = re.sub(r"\bOLO\s*\d+|boomnummer\s*\d+|\(\s*\d+\s*\)", " ", text, flags=re.I)
        items = extract(nlp(text), meta)
        ner_ft = obligation(items)
        det_ft = det[p["id"]]["felling_total"]
        is_fell = bool(re.search(r"\b(kap|vell|rooi|verplant)", text, re.I))
        if is_fell and det_ft is None:                 # deterministic residual
            resid += 1
            if ner_ft is not None:
                ner_resid_hit += 1
        if det_ft is not None and ner_ft is not None:
            agree += (det_ft == ner_ft)
            disagree += (det_ft != ner_ft)
        rows.append({"id": p["id"], "noord": p.get("noord", False), "abstract": p.get("abstract"),
                     "ner_items": items, "ner_obligation": ner_ft, "det_obligation": det_ft})
    with open(OUT, "w") as f:
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    print(f"corpus: {len(rows)} abstracts (nl_core_news_md + EntityRuler from vocab.py)\n")
    print(f"deterministic residual (felling permit, no det count): {resid}")
    print(f"  → spaCy recovers a count on:  {ner_resid_hit}/{resid} "
          f"({100*ner_resid_hit/resid:.0f}%)")
    print(f"\noverlap where both fire: agree {agree}, disagree {disagree} "
          f"({100*agree/(agree+disagree):.0f}% agreement)")
    total_ner = sum(1 for r in rows if r["ner_obligation"] is not None
                    and re.search(r"\b(kap|vell|rooi|verplant)", (r["abstract"] or ""), re.I))
    total_fell = sum(1 for r in rows if re.search(r"\b(kap|vell|rooi|verplant)",
                                                  (r["abstract"] or ""), re.I))
    print(f"\nspaCy obligation extracted overall: {total_ner}/{total_fell} felling permits "
          f"({100*total_ner/total_fell:.0f}%)  vs deterministic "
          f"{sum(1 for r in rows if r['det_obligation'] is not None)}/{total_fell}")
    print(f"\nwrote {OUT}")


if __name__ == "__main__":
    main()
