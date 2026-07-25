#!/usr/bin/env python3
"""Spike C — hand-authored GOLD obligation counts for the hard cases (human ground truth).

Obligation = trees to be FELLED or VERPLANT (transplant = felling, art. 1k), GRANTED, excluding
snoeien (pruning), herplant (fulfilment), and geweigerd/weigeren (refused). A tree named with its
species/appositive is ONE tree ("een boom, de Es" = 1). "waarvan" gives a breakdown, not a sum
("13 bomen, waarvan 7 ... en 6" = 13). None = genuinely no countable tree number in the text.

These are the 55 det↔spaCy disagreements + a 25-row residual sample — the set where the methods
differ, i.e. where accuracy is actually contested. Committed (unlike the regenerable *.jsonl) because
it is human judgment, not a probe output. Run `compare_methods.py` to score det/spaCy/LLM against it.
"""

# id → gold obligation count (int) or None (no countable felling number)
GOLD = {
    # --- disagreements: species appositive = one tree ---
    "gmb-2022-301686": 1, "gmb-2022-416620": 1, "gmb-2022-521583": 1, "gmb-2022-7327": 1,
    "gmb-2022-37575": 1, "gmb-2022-321330": 1, "gmb-2022-262613": 1, "gmb-2022-338528": 1,
    "gmb-2022-33704": 1, "gmb-2022-93942": 1, "gmb-2022-303123": 1, "gmb-2022-543983": 1,
    "gmb-2022-439740": 1,
    # --- disagreements: snoeien / weigeren excluded from obligation ---
    "gmb-2022-195847": 1, "gmb-2022-106681": 2, "gmb-2022-75569": 3, "gmb-2022-552190": 1,
    "gmb-2022-219191": 1, "gmb-2022-399417": 1, "gmb-2022-358670": 3, "gmb-2022-105296": 1,
    "gmb-2022-105323": 1, "gmb-2022-271572": 2, "gmb-2022-244581": 1, "gmb-2022-164643": 1,
    "gmb-2022-448325": 1, "gmb-2022-185849": 1, "gmb-2022-575245": 1, "gmb-2022-256549": 1,
    "gmb-2022-383044": 1, "gmb-2022-309826": 1, "gmb-2022-277203": 15, "gmb-2022-405076": 3,
    "gmb-2022-243716": 1, "gmb-2022-182114": 2, "gmb-2022-461112": 1,
    # --- disagreements: verplant IS felling; multi-count in one clause ---
    "gmb-2022-449048": 5, "gmb-2022-534271": 5, "gmb-2022-105285": 5, "gmb-2022-188367": 5,
    # --- disagreements: appositive list describes the counted total ---
    "gmb-2022-225191": 7, "gmb-2022-485407": 6, "gmb-2022-210988": 2, "gmb-2022-16697": 2,
    "gmb-2022-252501": 2, "gmb-2022-252479": 2, "gmb-2022-85593": 3, "gmb-2022-216627": 2,
    # --- disagreements: "waarvan" breakdown (both det & spaCy wrong), boomnummer leak, "of" ---
    "gmb-2022-470245": 13, "gmb-2022-448445": 13, "gmb-2022-353719": 5, "gmb-2022-89678": 4,
    "gmb-2022-444886": 2, "gmb-2022-444913": 2, "gmb-2022-507001": 2,
    # --- residual sample (det extracted nothing) ---
    "gmb-2022-333273": 22, "gmb-2022-45675": 44, "gmb-2022-12508": 3, "gmb-2022-369590": 7,
    "gmb-2022-514735": 3, "gmb-2022-17299": 1, "gmb-2022-240525": 1, "gmb-2022-517028": 1,
    "gmb-2022-262974": 1, "gmb-2022-397043": 1, "gmb-2022-231655": 1, "gmb-2022-187206": 1,
    "gmb-2022-205704": 1, "gmb-2022-365223": 1, "gmb-2022-378826": 1, "gmb-2022-376862": 1,
    "gmb-2022-290576": 1, "gmb-2022-476331": 1, "gmb-2022-266360": 1,
    "gmb-2022-37019": None, "gmb-2022-35055": None, "gmb-2022-548200": None,
    "gmb-2022-410228": None, "gmb-2022-84107": None, "gmb-2022-240106": None,
}
