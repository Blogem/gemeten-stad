#!/usr/bin/env python3
"""Spike C probe 2 — species / project / boomnummer mention yield (the NER lane).

How often does the permit prose name a *species*, a *project*, or a *boomnummer* —
the fuzzy, non-formulaic residue that a deterministic count parser (probe 1) can't
reach? These are the genuine targets for the spaCy NER lane, so this probe sizes
that lane: it is deliberately a *lower bound* from a seed lexicon, not a finished
extractor.

Measures, over the same reused ../spike-b abstracts (no re-harvest):
  - species: Dutch common names + Latin genera + cultivar/hybrid pattern
    ("Ulmus 'Dodoens'", "Tilia x europaea"); Dutch-vs-Latin-vs-cultivar split.
  - project: "project <Naam>" / "'<Naam>'" onderhoud/herinrichting references.
  - boomnummer: an explicit tree id ("boomnummer 346897") — a would-be join key.

Stdlib only; read-only (reads a local file). Run from this dir:
  python3 species_project.py
Reads ../spike-b/all_permits.jsonl. Writes mentions.jsonl.
"""
import collections
import json
import re

SRC = "../spike-b/all_permits.jsonl"
OUT = "mentions.jsonl"

# Dutch common tree names seen in / expected from the corpus (word-boundary matched).
DUTCH = ["iep", "es", "esdoorn", "eik", "linde", "plataan", "wilg", "populier",
         "kastanje", "beuk", "berk", "els", "meidoorn", "esch", "haagbeuk",
         "els", "esdoorn", "prunus", "sierkers", "els"]
# Latin genera (capitalized in prose).
LATIN = ["Ulmus", "Fraxinus", "Acer", "Quercus", "Tilia", "Platanus", "Populus",
         "Betula", "Fagus", "Salix", "Aesculus", "Prunus", "Alnus", "Carpinus",
         "Robinia", "Malus", "Sorbus", "Crataegus"]

DUTCH_RE = re.compile(r"\b(" + "|".join(sorted(set(DUTCH), key=len, reverse=True)) + r")\b", re.I)
LATIN_RE = re.compile(r"\b(" + "|".join(LATIN) + r")\b")
# a cultivar/hybrid: a capitalized genus followed by a quoted cultivar or "x epithet"
CULTIVAR_RE = re.compile(r"\b[A-Z][a-z]+\s+(?:'[^']+'|x\s+[a-z]+)")
PROJECT_RE = re.compile(r"\bproject\w*\s+['\"]?([A-Z][\w /()-]{2,40})", re.I)
BOOMNR_RE = re.compile(r"boom\s*(?:nummer|nr)\.?\s*(\d+)", re.I)


def scan(text):
    if not text:
        return {"dutch": [], "latin": [], "cultivar": False, "project": None, "boomnummers": []}
    dutch = sorted({m.group(1).lower() for m in DUTCH_RE.finditer(text)})
    latin = sorted({m.group(1) for m in LATIN_RE.finditer(text)})
    cultivar = bool(CULTIVAR_RE.search(text))
    proj = PROJECT_RE.search(text)
    bnrs = BOOMNR_RE.findall(text)
    return {"dutch": dutch, "latin": latin, "cultivar": cultivar,
            "project": proj.group(1).strip() if proj else None,
            "boomnummers": bnrs}


def report(rows):
    def block(label, subset):
        n = len(subset)
        if not n:
            return
        sp = [r for r in subset if r["m"]["dutch"] or r["m"]["latin"] or r["m"]["cultivar"]]
        du = [r for r in subset if r["m"]["dutch"]]
        la = [r for r in subset if r["m"]["latin"] or r["m"]["cultivar"]]
        pr = [r for r in subset if r["m"]["project"]]
        bn = [r for r in subset if r["m"]["boomnummers"]]
        print(f"\n=== {label} (n={n}) ===")
        print(f"  names ≥1 species:  {len(sp):4d}/{n} ({100*len(sp)/n:4.1f}%)")
        print(f"    of which Dutch:  {len(du):4d}   Latin/cultivar: {len(la):4d}")
        print(f"  names a project:   {len(pr):4d}/{n} ({100*len(pr)/n:4.1f}%)")
        print(f"  gives a boomnummer:{len(bn):4d}/{n} ({100*len(bn)/n:4.1f}%)")

    print(f"corpus: {len(rows)} abstracts")
    for scope_label, scope in [("CITYWIDE", rows), ("NOORD", [r for r in rows if r["noord"]])]:
        block(scope_label, scope)
    freq = collections.Counter()
    for r in rows:
        for d in r["m"]["dutch"]:
            freq[d] += 1
    print("\ntop Dutch species terms:", dict(freq.most_common(12)))


def main():
    permits = [json.loads(l) for l in open(SRC)]
    rows = []
    for p in permits:
        rows.append({"id": p["id"], "noord": p.get("noord", False),
                     "abstract": p.get("abstract"), "m": scan(p.get("abstract"))})
    with open(OUT, "w") as f:
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    report(rows)
    print(f"\nwrote {OUT} ({len(rows)} rows)")


if __name__ == "__main__":
    main()
