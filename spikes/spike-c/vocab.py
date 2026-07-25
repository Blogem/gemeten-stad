#!/usr/bin/env python3
"""Spike C — the seed vocabulary that feeds the spaCy EntityRuler.

STAND-IN for the real thing. In production this is a **SPARQL read of the SKOS graph**
(activity concepts from the regulation + species from IMBOR / Soortenregister + altLabels
mined from the corpus), exactly as `msr-graph` does it (`graph_reader.read_known_entities`
→ `seeding.build_matcher`): each concept's prefLabel/altLabels become EntityRuler patterns
whose `id` is the concept IRI, resolved back via `ent_id_`. Here we hand-list a slice so the
PoC runs without a triplestore — the *shape* (concept IRI + surface variants + kind) is what
Phase 2 will pull from the graph.

Two lessons from the corpus baked in: (1) the `nl_core_news_md` lemmatizer mis-normalizes
botanical terms ("essen"→"Essen", "iepen"→"ie"), so the vocab must carry **plural surface
variants** — it cannot lean on lemmatization; (2) *verplanten* is a felling by law
(Bomenverordening art. 1k, Spike C), so it maps to the same felling concept as kappen/vellen.
"""

# concept IRI → (kind, activity_type or number_hint, [surface variants])
#   kind ACTIVITY: activity_type ∈ {felling, replant}
#   kind TREE:     number_hint   ∈ {sg, pl}  (drives implicit-1)
VOCAB = [
    # --- activity concepts (all felling verbs collapse to one obligation concept) ---
    ("gs:act/vellen", "ACTIVITY", "felling",
     ["vellen", "vel", "geveld", "kappen", "kap", "gekapt", "rooien", "rooi", "gerooid",
      "verplanten", "verplant", "verplaatsen", "kandelaberen"]),
    ("gs:act/herplanten", "ACTIVITY", "replant",
     ["herplanten", "herplant", "planten", "aanplanten", "aanplant"]),
    # non-obligation activities: the tree stays → no felling, no herplantplicht. Recognized so
    # their trees are attributed here and EXCLUDED from the obligation (snoeien = pruning).
    ("gs:act/snoeien", "ACTIVITY", "other",
     ["snoeien", "snoei", "gesnoeid", "weigeren", "geweigerd", "handhaven"]),
    # --- generic tree nouns ---
    ("gs:tree/boom", "TREE", "sg", ["boom", "houtopstand"]),
    ("gs:tree/boom#pl", "TREE", "pl", ["bomen", "houtopstanden"]),
    # --- species concepts (Dutch sg + pl + Latin genus; a slice of Soortenregister/IMBOR) ---
    ("gs:species/ulmus", "TREE", "sg", ["iep", "Ulmus"]),
    ("gs:species/ulmus#pl", "TREE", "pl", ["iepen"]),
    ("gs:species/fraxinus", "TREE", "sg", ["es", "Fraxinus"]),
    ("gs:species/fraxinus#pl", "TREE", "pl", ["essen"]),
    ("gs:species/acer", "TREE", "sg", ["esdoorn", "Acer"]),
    ("gs:species/acer#pl", "TREE", "pl", ["esdoorns", "esdoornen"]),
    ("gs:species/quercus", "TREE", "sg", ["eik", "Quercus"]),
    ("gs:species/quercus#pl", "TREE", "pl", ["eiken"]),
    ("gs:species/tilia", "TREE", "sg", ["linde", "Tilia"]),
    ("gs:species/tilia#pl", "TREE", "pl", ["linden"]),
    ("gs:species/platanus", "TREE", "sg", ["plataan", "Platanus"]),
    ("gs:species/platanus#pl", "TREE", "pl", ["platanen"]),
    ("gs:species/salix", "TREE", "sg", ["wilg", "treurwilg", "Salix"]),
    ("gs:species/salix#pl", "TREE", "pl", ["wilgen"]),
    ("gs:species/populus", "TREE", "sg", ["populier", "Populus"]),
    ("gs:species/populus#pl", "TREE", "pl", ["populieren"]),
    ("gs:species/aesculus", "TREE", "sg", ["kastanje", "paardenkastanje", "Aesculus"]),
    ("gs:species/aesculus#pl", "TREE", "pl", ["kastanjes", "paardenkastanjes"]),
    ("gs:species/fagus", "TREE", "sg", ["beuk", "Fagus"]),
    ("gs:species/fagus#pl", "TREE", "pl", ["beuken"]),
    ("gs:species/betula", "TREE", "sg", ["berk", "Betula"]),
    ("gs:species/betula#pl", "TREE", "pl", ["berken"]),
    ("gs:species/alnus", "TREE", "sg", ["els", "Alnus"]),
    ("gs:species/alnus#pl", "TREE", "pl", ["elzen"]),
    ("gs:species/crataegus", "TREE", "sg", ["meidoorn", "Crataegus"]),
    ("gs:species/pinus", "TREE", "sg", ["den", "Pinus"]),
    ("gs:species/pinus#pl", "TREE", "pl", ["dennen"]),
    ("gs:species/picea", "TREE", "sg", ["spar", "conifeer", "Picea"]),
    ("gs:species/picea#pl", "TREE", "pl", ["sparren", "coniferen"]),
    ("gs:species/cedrus", "TREE", "sg", ["ceder", "Cedrus"]),
]


def patterns():
    """EntityRuler phrase patterns: {label, pattern, id} — msr-graph seeding shape."""
    pats, meta = [], {}
    for iri, kind, hint, forms in VOCAB:
        meta[iri] = (kind, hint)
        for form in forms:
            pats.append({"label": kind, "pattern": form, "id": iri})
    return pats, meta
