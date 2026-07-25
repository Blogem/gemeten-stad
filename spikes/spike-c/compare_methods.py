#!/usr/bin/env python3
"""Spike C probe 5c — score deterministic vs spaCy vs LLM against the gold set.

Reads the hand-authored GOLD (gold_labels.py) and each method's obligation count:
  - deterministic parser  → activities.jsonl   (felling_total)
  - spaCy EntityRuler     → ner_counts.jsonl   (ner_obligation)
  - LLM (if run)          → llm_counts.jsonl   (llm_obligation)
and prints exact-match accuracy on the contested set — the answer to "does the LLM
dependency significantly beat the deterministic binders?".

Run from this dir:  python3 compare_methods.py   (LLM column appears once extract_llm.py has run)
"""
import json
import os

from gold_labels import GOLD


def load(path, key):
    if not os.path.exists(path):
        return None
    return {json.loads(l)["id"]: json.loads(l).get(key) for l in open(path)}


def main():
    det = load("activities.jsonl", "felling_total")
    ner = load("ner_counts.jsonl", "ner_obligation")
    llm = load("llm_counts.jsonl", "llm_obligation")
    methods = [("deterministic", det), ("spaCy+vocab", ner)]
    if llm is not None:
        methods.append(("LLM", llm))

    rows = []
    for gid, gold in GOLD.items():
        rows.append((gid, gold, {name: (m or {}).get(gid) for name, m in methods}))

    n = len(rows)
    print(f"gold set: {n} hard cases (55 disagreements + 25 residual)\n")
    print(f"{'method':16} {'exact':>8} {'acc':>6}   (obligation-count exact match vs gold)")
    for name, _ in methods:
        exact = sum(1 for _, gold, preds in rows if preds[name] == gold)
        print(f"{name:16} {exact:>4}/{n:<3} {100*exact/n:5.0f}%")

    print("\nper-case (gold → each method; ✗ = wrong):")
    hdr = "  ".join(f"{name[:12]:>12}" for name, _ in methods)
    print(f"  {'id':18} {'gold':>4}   {hdr}")
    for gid, gold, preds in rows:
        cells = []
        for name, _ in methods:
            p = preds[name]
            mark = "" if p == gold else " ✗"
            cells.append(f"{str(p):>12}{mark}")
        print(f"  {gid:18} {str(gold):>4}   " + "  ".join(cells))


if __name__ == "__main__":
    main()
