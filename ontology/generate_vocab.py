#!/usr/bin/env python3
"""Generate ontology/vocab.ttl — the SKOS domain vocabulary for the tree-audit domain.

vocab.ttl is a CHECKED-IN artifact, regenerated on demand by this script — NOT rebuilt
each pipeline run (IMPLEMENTATION_PLAN.md §3 "living vocab": seeded from sources, grown from
the corpus). Run it after `load/bomen` and `load/geo` have populated Postgres, then commit the
result.

What it seeds (design.md D7, all city-wide):
  • Legal top  — hand-authored from the two CVDR regulation texts, each concept dct:source-d to
    its article (Bomenverordening 2014 CVDR323217; Compensatie en herplant CVDR697591). NOT
    invented.
  • Activities — the single felling concept (verplanten ≡ vellen, Bomenverordening art. 1k /
    Spike C) with rooien/kappen/kandelaberen/verplanten as altLabels, plus herplanten.
  • Status    — boomAanwezigheid (the fulfilment axis, Spike C), read from the loaded rows.
  • Species   — TWO-LEVEL hierarchy (user decision): one concept per city-wide
    stamgegevens.soortnaamTop genus (~185; Dutch prefLabel + Latin genus + authored plural where
    the top is a Dutch common noun), with the ~1.5k distinct soortnaam cultivars as skos:broader
    children. The Latin-only tops (Sorbus, Magnolia, …) carry no Dutch plural (spec-exempt).

Places are NOT seeded (user decision). The vocab holds concepts whose *meaning* is not already
in the value store (NER surface forms, regulation definitions, semantic hierarchy). A gebieden
buurt's meaning IS its geometry + its buurt→wijk→stadsdeel membership — all authoritative in
PostGIS (gebieden_buurten: naam + code + geom + ligtinwijkid). A SKOS name→code shadow would add
nothing and invite drift, and any real place question needs PostGIS anyway (geometry, containment,
counts). Places live as gs:Place instance nodes keyed by their gebieden/BAG code, resolving to
the value store; the analytics agent gets a value-store query capability for locations (Phase 2).

Deferred (recorded in the vocab header, so scale-up is planned not surprising):
  • boomgebreken — NOT a stamgegevens field; it is the separate un-ingested `gebrekregistratie`
    dataset. Landing it is a load/bomen (P7) scope addition, not a p8 task.
  • corpus-mined surface variants (abbreviations, "t.h.v.", cultivar phrasings) — Phase 2, mines
    the Noord corpus first.
  • a second intervention type's vocabulary slices — Phase 4 (IMPLEMENTATION_PLAN §6).
  • skos:exactMatch alignment to TOOI/IMBOR/Soortenregister — best-effort; v0 keeps local IRIs +
    dct:source (D7 permits this, no blocker). No re-minted parallel identifiers.

DB access: runs `psql`. Override the command with GS_PSQL (e.g.
`GS_PSQL="docker exec -i compose-db-1 psql -U gs -d gemeten_stad"`); otherwise it uses
`psql "$GS_DATABASE_URL"`. Requires a DB where load/bomen + load/geo have run.

Usage:  python3 ontology/generate_vocab.py > ontology/vocab.ttl
"""
import os
import re
import shlex
import subprocess
import sys

# ---------------------------------------------------------------------------
# Hand-authored Dutch surface forms, keyed by Latin genus (lower-case).
# prefLabel comes from the source soortnaamTop; these ENRICH altLabels for NER.
# The ~20 common species anchor on Spike C's corpus-attested forms
# (spikes/spike-c/vocab.py); plurals verified against OpenTaal where an entry exists
# (github.com/OpenTaal/opentaal-wordlist). Compound/rare plurals OpenTaal omits
# (amberbomen, vleugelnoten, watercypressen, venijnbomen) are morphologically derived on
# the head noun (bomen/noten/cypressen — all present in OpenTaal). This is load-bearing:
# the nl_core_news_md lemmatizer mis-normalizes botanical plurals (Spike C Finding 5), so
# the vocab must carry the plural surface forms explicitly.
#   genus -> {"plural": [nl plural forms], "extra": [extra nl surface forms, sg or pl]}
SPECIES_NL = {
    "ulmus":       {"plural": ["iepen"],                       "extra": []},
    "acer":        {"plural": ["esdoorns"],                    "extra": ["esdoorn"]},
    "tilia":       {"plural": ["linden", "lindes"],            "extra": ["lindeboom", "lindebomen"]},
    "fraxinus":    {"plural": ["essen"],                       "extra": []},
    "quercus":     {"plural": ["eiken"],                       "extra": ["eikenboom", "eikenbomen"]},
    "platanus":    {"plural": ["platanen"],                    "extra": []},
    "salix":       {"plural": ["wilgen"],                      "extra": ["treurwilg"]},
    "populus":     {"plural": ["populieren"],                  "extra": []},
    "alnus":       {"plural": ["elzen"],                       "extra": []},
    "prunus":      {"plural": ["kersen"],                      "extra": ["sierkers"]},
    "betula":      {"plural": ["berken"],                      "extra": []},
    "carpinus":    {"plural": ["haagbeuken"],                  "extra": []},
    "robinia":     {"plural": ["acacia's"],                    "extra": []},
    "crataegus":   {"plural": ["meidoorns"],                   "extra": []},
    "malus":       {"plural": ["appels"],                      "extra": ["appelboom", "appelbomen", "sierappel"]},
    "aesculus":    {"plural": ["paardenkastanjes"],            "extra": ["kastanje", "kastanjes"]},
    "gleditsia":   {"plural": ["valse christusdoorns"],        "extra": ["christusdoorn"]},
    "pyrus":       {"plural": ["peren"],                       "extra": ["sierpeer"]},
    "fagus":       {"plural": ["beuken"],                      "extra": []},
    "liquidambar": {"plural": ["amberbomen"],                  "extra": []},
    "pterocarya":  {"plural": ["vleugelnoten"],                "extra": []},
    "metasequoia": {"plural": ["watercypressen"],              "extra": []},
    "ilex":        {"plural": ["hulsten"],                     "extra": []},
    "ginkgo":      {"plural": ["notenbomen"],                  "extra": ["notenboom", "japanse notenbomen"]},
    "taxus":       {"plural": ["venijnbomen"],                 "extra": []},
    # Genera arriving Latin-only in soortnaamTop but common in permit prose (Spike C): the Dutch
    # forms enrich altLabels so NER matches "den"/"spar"/"ceder"; prefLabel stays the Latin source.
    "pinus":       {"plural": ["dennen"],                      "extra": ["den"]},
    "picea":       {"plural": ["sparren", "coniferen"],        "extra": ["spar", "conifeer"]},
    "cedrus":      {"plural": ["ceders"],                      "extra": ["ceder"]},
}

NS_ID = "http://gemetenstad.nl/id/"
SCHEME = "sch:tree-audit"
CVDR_BV = "https://lokaleregelgeving.overheid.nl/CVDR323217/2"      # Bomenverordening 2014
CVDR_CH = "https://lokaleregelgeving.overheid.nl/CVDR697591"        # Compensatie en herplant


def psql(sql):
    """Run SQL, return rows as lists of columns (tab-separated, no header)."""
    cmd = os.environ.get("GS_PSQL")
    if cmd:
        argv = shlex.split(cmd)
    else:
        dsn = os.environ.get("GS_DATABASE_URL")
        if not dsn:
            sys.exit("generate_vocab: set GS_PSQL or GS_DATABASE_URL")
        argv = ["psql", dsn]
    argv += ["-tAF", "\t", "-c", sql]
    out = subprocess.run(argv, capture_output=True, text=True, check=True).stdout
    return [line.split("\t") for line in out.splitlines() if line.strip()]


def slug(s):
    """A stable IRI-safe slug from a source string."""
    s = s.lower()
    s = s.replace("'", "").replace("`", "")
    s = re.sub(r"[^a-z0-9]+", "-", s).strip("-")
    return s


def esc(s):
    """Escape a string literal for Turtle (double-quoted, single line)."""
    return s.replace("\\", "\\\\").replace('"', '\\"')


def lit(s):
    return '"' + esc(s) + '"'


TOP_RE = re.compile(r"^(.*?)\s*\(([^)]+)\)\s*$")


def parse_top(top):
    """soortnaamTop -> (dutch_prefLabel or None, latin_genus).

    'Iep (Ulmus)'                    -> ('iep', 'Ulmus')
    'Valse christusdoorn (Gleditsia triacanthos)' -> ('valse christusdoorn', 'Gleditsia')
    'Sorbus'                         -> (None, 'Sorbus')
    'Onbekend'                       -> (None, None)   (placeholder)
    """
    if top == "Onbekend":
        return (None, None)
    m = TOP_RE.match(top)
    if m:
        dutch = m.group(1).strip()
        latin_genus = m.group(2).strip().split()[0]  # first word = genus
        return (dutch.lower(), latin_genus)
    # no parens: the value IS the Latin genus
    return (None, top.strip().split()[0])


def emit(s):
    print(s)


def header():
    emit('''# De Gemeten Stad — SKOS domain vocabulary (v0)
# ============================================================================
# GENERATED by ontology/generate_vocab.py from the loaded Postgres value store +
# hand-authored legal/activity concepts. Regenerate with that script; do not hand-edit.
#
# Seeded from authoritative sources, not invented (design.md D7). Identity rests on
# codes/IRIs (skos:notation), never on names. Concepts are city-wide.
#
# DEFERRED axes (recorded so scale-up is planned, not a surprise — D8):
#   • boomgebreken (tree defects) — NOT a stamgegevens field; it is the separate
#     un-ingested `gebrekregistratie` dataset. Landing it is a load/bomen (P7) scope
#     addition, not a p8-modeling task.
#   • corpus-mined surface variants (abbreviations, "t.h.v.", cultivar phrasings) —
#     Phase 2 feedback loop, which mines the Noord backfill corpus first.
#   • a second intervention type's vocabulary slices — Phase 4 (IMPLEMENTATION_PLAN §6).
#   • skos:exactMatch alignment to TOOI/IMBOR/Soortenregister — best-effort; v0 keeps
#     local IRIs + dct:source (D7 permits this). No re-minted parallel identifiers.
#
# Grammatical plurals ARE authored city-wide in v0 (no inflection gap).
#
# PLACES ARE NOT SEEDED as concepts. A gebieden/CBS place's meaning is its geometry + admin
# hierarchy, authoritative in PostGIS (gebieden_buurten: naam+code+geom+ligtinwijkid). The vocab
# holds only concepts whose meaning is NOT already in the value store; a SKOS name→code shadow
# adds nothing and any real place query needs PostGIS anyway. Places live as gs:Place instance
# nodes keyed by their code; the analytics agent queries the locations data directly (Phase 2).
# ============================================================================

@prefix gs:   <http://gemetenstad.nl/ns#> .
@prefix data: <http://gemetenstad.nl/id/> .
@prefix sch:  <http://gemetenstad.nl/id/scheme/> .
@prefix con:  <http://gemetenstad.nl/id/concept/> .
@prefix act:  <http://gemetenstad.nl/id/activity/> .
@prefix sts:  <http://gemetenstad.nl/id/status/> .
@prefix sp:   <http://gemetenstad.nl/id/species/> .
@prefix skos: <http://www.w3.org/2004/02/skos/core#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix dct:  <http://purl.org/dc/terms/> .
@prefix xsd:  <http://www.w3.org/2001/XMLSchema#> .

sch:tree-audit a skos:ConceptScheme ;
    skos:prefLabel "Gemeten Stad tree-audit vocabulary"@en ;
    dct:source <%s> , <%s> .
''' % (CVDR_BV, CVDR_CH))


def legal_and_activity():
    emit("# ===== Activity concepts (Bomenverordening art. 1k; Spike C: verplanten ≡ vellen) =====\n")
    emit('''act:vellen a skos:Concept ;
    skos:inScheme sch:tree-audit ;
    skos:prefLabel "vellen"@nl ;
    skos:altLabel "kappen"@nl , "rooien"@nl , "kandelaberen"@nl , "verplanten"@nl ,
                  "vel"@nl , "kap"@nl , "gekapt"@nl , "gerooid"@nl , "geveld"@nl , "verplant"@nl ;
    skos:notation "Vellen (boom verwijderen)" ;
    skos:definition "Rooien, kappen, kandelaberen of verplanten, evenals handelingen die de dood of ernstige beschadiging of ontsiering van de houtopstand tot gevolg kunnen hebben."@nl ;
    skos:scopeNote "One felling concept: verplanten is a velling by law (Bomenverordening art. 1k) and the registry has no separate Verplanten value (Spike C). A transplant is counted with the felling total and flagged gs:transplantOrigin."@nl ;
    dct:source <%s> .

act:herplanten a skos:Concept ;
    skos:inScheme sch:tree-audit ;
    skos:prefLabel "herplanten"@nl ;
    skos:altLabel "herplant"@nl , "planten"@nl , "aanplanten"@nl , "aanplant"@nl , "herplanting"@nl ;
    skos:scopeNote "The replant/fulfilment side. Counts on the replant side only — NEVER in the felling obligation total (Spike C)."@nl ;
    dct:source <%s> .
''' % (CVDR_BV, CVDR_BV))

    emit("# ===== Legal-top concepts (derived from the two CVDR texts; dct:source per article) =====\n")
    legal = [
        ("boom", "boom", ["stamomtrek"],
         "Een houtachtig, opgaand gewas, levend of afgestorven, met een omtrek van de stam van minimaal 31 centimeter op 130 centimeter hoogte boven het maaiveld; bij meerstammigheid geldt de omtrek van de dikste stam.",
         "Bomenverordening 2014 art. 1b — the 31 cm stamomtrek threshold that makes a tree permit-relevant.",
         CVDR_BV),
        ("houtopstand", "houtopstand", ["houtopstanden"],
         "Één of meer bomen, hakhout, boomvormers of andere houtachtige gewassen die onderdeel uitmaken van een houtwal, (lint)begroeiing of bosplantsoen, met de onder art. 1b genoemde minimale omtrek.",
         "Bomenverordening 2014 art. 1f.",
         CVDR_BV),
        ("kandelaberen", "kandelaberen", [],
         "Het voor meer dan 20% gelijkmatig weghalen van de takken die de kroon van een boom vormen.",
         "Bomenverordening 2014 art. 1h — a form of vellen (art. 1k).",
         CVDR_BV),
        ("beschermwaardige-houtopstand", "beschermwaardige houtopstand", ["monumentale boom", "waardevolle houtopstand"],
         "Houtopstand die geplaatst is op de lijst als bedoeld in artikel 10.",
         "Bomenverordening 2014 art. 1a / art. 10 — the protected-tree list (colloquially 'monumentale boom').",
         CVDR_BV),
        ("monetaire-boomwaarde", "monetaire boomwaarde", ["boomwaarde"],
         "De financiële waarde van een boom of houtopstand, getaxeerd volgens de actuele richtlijnen van de Nederlandse Vereniging van Taxateurs van Bomen (NVTB).",
         "Bomenverordening 2014 art. 1j — the value stored in the herplantfonds when replanting in kind is impossible.",
         CVDR_BV),
        ("herplantplicht", "herplantplicht", ["herplantverplichting"],
         "Het college verbindt aan de vergunning het voorschrift dat binnen een te bepalen termijn en volgens aanwijzingen wordt herplant, tenzij zwaarwegende argumenten zich daartegen verzetten.",
         "Bomenverordening 2014 art. 7 lid 1 — the replanting obligation the audit tests.",
         CVDR_BV),
        ("herplantfonds", "herplantfonds", ["herplantregeling"],
         "Fonds waarin de monetaire boomwaarde van de gevelde houtopstand wordt gestort wanneer herplant ter plaatse niet mogelijk is (art. 7 lid 3); de gelden worden geoormerkt voor compensatie elders.",
         "Bomenverordening 2014 art. 7 lid 3.",
         CVDR_BV),
        ("herplantcompensatie", "herplantcompensatie", ["compensatie", "boomjaren"],
         "Het aantal te herplanten bomen wordt bepaald door de leeftijd van de gevelde boom in leeftijdsklassen van 8 jaar (Omrekentabel). Een vervangende boom heeft standaard een stamomtrek van 18–20 cm op 1 m hoogte; men mag minder maar dikkere bomen herplanten mits dezelfde fysieke boomwaarde.",
         "Compensatie en herplant van bomen (beleidsregel bij art. 7 Bomenverordening 2014) — the Omrekentabel. NB: the ladder is keyed on the felled tree's AGE (leeftijdsklassen), not its diameter.",
         CVDR_CH),
    ]
    for cid, pref, alts, defn, note, src in legal:
        alt_ttl = " ;\n    skos:altLabel " + " , ".join(f'{lit(a)}@nl' for a in alts) if alts else ""
        emit(f'''con:{cid} a skos:Concept ;
    skos:inScheme sch:tree-audit ;
    skos:prefLabel {lit(pref)}@nl{alt_ttl} ;
    skos:definition {lit(defn)}@nl ;
    skos:scopeNote {lit(note)}@nl ;
    dct:source <{src}> .
''')

    # The 5 leeftijdsklassen of the Omrekentabel (user decision: mint them, faithful to source).
    emit("# ----- Omrekentabel: replant obligation by felled-tree age class (CVDR697591) -----\n")
    klassen = [
        ("tot-15", "leeftijdsklasse tot en met 15 jaar", "1 te herplanten boom (stamomtrek 18–20 cm)."),
        ("16-23", "leeftijdsklasse 16–23 jaar", "2 te herplanten bomen (18–20 cm), of één boom van 20–25 cm."),
        ("24-31", "leeftijdsklasse 24–31 jaar", "3 te herplanten bomen (18–20 cm), of één boom van 25–30 cm."),
        ("32-39", "leeftijdsklasse 32–39 jaar", "4 te herplanten bomen (18–20 cm), of één boom van 30–35 cm."),
        ("40-47", "leeftijdsklasse 40–47 jaar", "5 te herplanten bomen (18–20 cm), of één boom van 35–40 cm."),
    ]
    for kid, pref, note in klassen:
        emit(f'''con:leeftijdsklasse-{kid} a skos:Concept ;
    skos:inScheme sch:tree-audit ;
    skos:prefLabel {lit(pref)}@nl ;
    skos:broader con:herplantcompensatie ;
    skos:scopeNote {lit(note + " Omrekentabel, Compensatie en herplant van bomen (CVDR697591).")}@nl ;
    dct:source <{CVDR_CH}> .
''')


def status():
    rows = psql("SELECT DISTINCT raw->>'boomAanwezigheid' FROM kapenherplant "
                "WHERE raw->>'boomAanwezigheid' IS NOT NULL AND raw->>'boomAanwezigheid' <> '' "
                "ORDER BY 1")
    if not rows:
        return
    emit("# ===== Status: boomAanwezigheid (the fulfilment axis, Spike C) =====\n")
    for (val,) in rows:
        emit(f'''sts:{slug(val)} a skos:Concept ;
    skos:inScheme sch:tree-audit ;
    skos:prefLabel {lit(val)}@nl ;
    skos:notation {lit(val)} ;
    skos:scopeNote "kapenherplant.boomAanwezigheid value — the observed replant-fulfilment signal."@nl .
''')


def species():
    tops = [r[0] for r in psql(
        "SELECT DISTINCT raw->>'soortnaamTop' FROM stamgegevens "
        "WHERE raw->>'soortnaamTop' IS NOT NULL ORDER BY 1")]
    # genus (lower) -> {"iri": ..., "pref": (label,lang), "latin": genus, "notations": set, "nl_extra": set}
    genera = {}
    for top in tops:
        dutch, latin_genus = parse_top(top)
        if latin_genus is None:               # 'Onbekend' placeholder
            key, iri_slug, pref, lang, latin = "onbekend", "onbekend", "onbekend", "nl", None
        else:
            key = latin_genus.lower()
            iri_slug = key
            if dutch:
                pref, lang, latin = dutch, "nl", latin_genus
            else:
                pref, lang, latin = latin_genus, None, latin_genus
        g = genera.setdefault(key, {"slug": iri_slug, "pref": (pref, lang), "latin": latin,
                                    "notations": set(), "dutch": None})
        g["notations"].add(top)
        # a Dutch prefLabel wins over a Latin one if two source strings collapse to one genus
        if dutch and g["pref"][1] != "nl":
            g["pref"] = (dutch, "nl")
        if dutch:
            g["dutch"] = dutch

    emit("# ===== Species — two-level: soortnaamTop genus (top) + soortnaam cultivars (children) =====")
    emit("# prefLabel = Dutch common name where soortnaamTop supplies one, else the Latin genus.")
    emit("# Dutch-common-noun tops carry an authored plural altLabel (NER); Latin-only tops do not.\n")
    for key in sorted(genera):
        g = genera[key]
        pref, lang = g["pref"]
        pref_ttl = f'{lit(pref)}@{lang}' if lang else lit(pref)
        alt = []
        if g["latin"] and (lang == "nl"):
            alt.append(lit(g["latin"]))              # Latin genus as altLabel when pref is Dutch
        nl = SPECIES_NL.get(key)
        if nl:
            for p in nl["plural"]:
                alt.append(f'{lit(p)}@nl')
            for e in nl["extra"]:
                alt.append(f'{lit(e)}@nl')
        body = [f'sp:{g["slug"]} a skos:Concept ;',
                f'    skos:inScheme sch:tree-audit ;',
                f'    skos:prefLabel {pref_ttl}']
        # dedup, and drop any altLabel identical to the prefLabel (e.g. esdoorn/Acer)
        alt = [a for a in dict.fromkeys(alt) if a != pref_ttl]
        if alt:
            body[-1] += ' ;'
            body.append('    skos:altLabel ' + " , ".join(alt))
        # source notation(s): the raw soortnaamTop string(s) = identity from the data
        body[-1] += ' ;'
        body.append('    skos:notation ' + " , ".join(lit(n) for n in sorted(g["notations"])) + ' .')
        emit("\n".join(body) + "\n")

    # cultivar children (skos:broader the genus top), derived from the loaded mapping.
    rows = psql("SELECT DISTINCT soortnaam, raw->>'soortnaamTop' FROM stamgegevens "
                "WHERE soortnaam IS NOT NULL AND raw->>'soortnaamTop' IS NOT NULL ORDER BY 2,1")
    emit("# ----- Cultivar children (soortnaam), skos:broader their genus top -----\n")
    for soortnaam, top in rows:
        _, latin_genus = parse_top(top)
        key = latin_genus.lower() if latin_genus else "onbekend"
        # a top whose only source is the cultivar itself (soortnaam == genus) needs no child
        if soortnaam.strip().lower() == key:
            continue
        emit(f'''sp:{slug(soortnaam)} a skos:Concept ;
    skos:inScheme sch:tree-audit ;
    skos:prefLabel {lit(soortnaam)} ;
    skos:notation {lit(soortnaam)} ;
    skos:broader sp:{key} .
''')


def caveats():
    # The four caveat terms are gs:Caveat individuals in ontology.ttl; mirror them here as
    # skos:Concept members so the controlled-value-set (shapes 3.3) is uniformly SKOS-backed.
    emit("# ===== Caveat flags (also gs:Caveat individuals in ontology.ttl) =====\n")
    cav = [("unresolvedLocation", "unresolved location"),
           ("timeMismatch", "time mismatch"),
           ("weakLink", "weak link"),
           ("transplantOrigin", "transplant origin")]
    for cid, label in cav:
        emit(f'''gs:{cid} a skos:Concept ;
    skos:inScheme sch:tree-audit ;
    skos:prefLabel {lit(label)}@en .
''')


def main():
    header()
    legal_and_activity()
    status()
    species()
    caveats()


if __name__ == "__main__":
    main()
