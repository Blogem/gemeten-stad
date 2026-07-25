#!/usr/bin/env python3
"""Spike C probe 4 — does the full document body add anything the abstract lacks?

Spike A found the permit *body* is a stub for conditions/termijn. Spike C's narrower
question: for the extraction targets — counts / activities / species / project — does
the SRU `abstract` carry everything the full document body carries? If yes, Phase 2
parses the abstract alone (no per-document XML fetch — a real cost saving).

Method: pick a deterministic set of hard-shape publications from probe 1/2 output
(2+ activities, spelled-out counts naive missed, species named, project named), fetch
the full body XML (repository.overheid.nl/frbr/…/<id>.xml), strip to plain text, and
re-run the SAME parser (parse_activities) + species/project scan (species_project) on
the body. Report whether the body yields a felling count / activity / species / project
the abstract did not.

Stdlib only; read-only GETs. Run from this dir (after probes 1 & 2):
  python3 body_vs_abstract.py
Reads activities.jsonl, mentions.jsonl. Writes bodies_sample.jsonl.
"""
import json
import re
import urllib.request

from parse_activities import parse_abstract, felling_total
from species_project import scan

OUT = "bodies_sample.jsonl"


def pick_ids():
    acts = {json.loads(l)["id"]: json.loads(l) for l in open("activities.jsonl")}
    ment = {json.loads(l)["id"]: json.loads(l) for l in open("mentions.jsonl")}
    multi = [r["id"] for r in acts.values()
             if len({a["verb"] for a in r["activities"]}) >= 2]
    spelled = [r["id"] for r in acts.values()
               if r["naive_count"] is None and r["felling_total"] is not None]
    species = [i for i, m in ment.items() if m["m"]["dutch"] or m["m"]["latin"]]
    project = [i for i, m in ment.items() if m["m"]["project"]]
    # deterministic: sort each bucket, take a few, dedup preserving order
    chosen, seen = [], set()
    for bucket in (sorted(multi)[:5], sorted(spelled)[:4],
                   sorted(species)[:3], sorted(project)[:3]):
        for i in bucket:
            if i not in seen:
                seen.add(i)
                chosen.append(i)
    return chosen, acts, ment


def fetch_body(gmb_id):
    year = gmb_id.split("-")[1]
    url = (f"https://repository.overheid.nl/frbr/officielepublicaties/gmb/"
           f"{year}/{gmb_id}/1/xml/{gmb_id}.xml")
    try:
        with urllib.request.urlopen(url, timeout=90) as r:
            xml = r.read().decode("utf-8", "replace")
    except Exception:  # noqa: BLE001
        return None
    return re.sub(r"\s+", " ", re.sub(r"<[^>]+>", " ", xml)).strip()


def main():
    ids, acts, ment = pick_ids()
    rows, adds = [], 0
    print(f"fetching {len(ids)} hard-shape bodies…\n")
    for gid in ids:
        body = fetch_body(gid)
        a = acts[gid]
        abs_ft = a["felling_total"]
        abs_sp = set(ment[gid]["m"]["dutch"]) | set(ment[gid]["m"]["latin"])
        abs_pr = ment[gid]["m"]["project"]
        if body is None:
            print(f"  {gid}: body fetch FAILED")
            rows.append({"id": gid, "body": None})
            continue
        body_ft = felling_total(parse_abstract(body))
        body_scan = scan(body)
        body_sp = set(body_scan["dutch"]) | set(body_scan["latin"])
        body_pr = body_scan["project"]
        # does the body add a signal the abstract lacked?
        added = []
        if abs_ft is None and body_ft is not None:
            added.append(f"count({body_ft})")
        if body_sp - abs_sp:
            added.append(f"species({sorted(body_sp - abs_sp)})")
        if not abs_pr and body_pr:
            added.append(f"project({body_pr!r})")
        adds += bool(added)
        print(f"  {gid} [{len(body):5d} chars] abstract_ft={abs_ft} body_ft={body_ft}"
              f"  ADDS: {added or 'nothing new'}")
        rows.append({"id": gid, "abstract_felling": abs_ft, "body_felling": body_ft,
                     "abstract_species": sorted(abs_sp), "body_species": sorted(body_sp),
                     "abstract_project": abs_pr, "body_project": body_pr,
                     "body_adds": added, "body_chars": len(body)})
    with open(OUT, "w") as f:
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    print(f"\n=== VERDICT: body added a new extraction signal in {adds}/{len(ids)} sampled docs ===")
    print(f"wrote {OUT}")


if __name__ == "__main__":
    main()
