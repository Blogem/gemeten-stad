#!/usr/bin/env bash
# Spike E — drive the four required checks + two risk probes against a containerised
# Apache Jena Fuseki, and print a PASS/FAIL gate (spike-d style).
#
#   1. load a tiny RDF-star sample (base edge asserted + confidence annotation)
#   2. SPARQL-star read: pull << … >> :confidence back off the fuzzy edge
#   3. SHACL: accept a well-formed instance, reject malformed ones — via BOTH the
#      Fuseki SHACL endpoint AND the independent Jena CLI (`shacl validate`)
#   4. write/read run-stamped named graphs and query ACROSS graphs
#   probe A: is the confidence-presence rule enforceable over RDF-star? (and does
#            Jena #3503 — SHACL passing data merely because RDF-star is present — bite?)
#   probe B: are run-stamped named graphs ergonomic as the routine load/derive output?
#
# Idempotent: re-running is a no-op for the load unless --reset (or the store is empty).
# Usage:  ./run.sh [--reset]
set -euo pipefail
cd "$(dirname "$0")"

RESET="${1:-}"
BASE="http://localhost:3030/ds"
ADMIN="admin:${FUSEKI_ADMIN_PASSWORD:-spike}"
RUN1="http://gemeten-stad.nl/run/load-2022q4-001"

fails=0
pass() { printf '  \033[32mPASS\033[0m  %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m  %s\n' "$1"; fails=$((fails+1)); }
check() { if [ "$2" = "$3" ]; then pass "$1 ($2)"; else fail "$1 (got '$2', want '$3')"; fi; }

# HTTP helpers
# SPARQL query → CSV; strip the CR from Fuseki's CRLF line endings so string compares work
q()   { curl -s -u "$ADMIN" -H 'Accept: text/csv' --data-urlencode "query=$1" "$BASE/sparql" | tr -d '\r'; }
upd() { curl -s -u "$ADMIN" --data-urlencode "update=$1" "$BASE/update" >/dev/null; }
# validate a named graph against shapes.ttl via the Fuseki SHACL endpoint; echoes true/false
shacl_http() { curl -s -u "$ADMIN" -X POST -H 'Content-Type: text/turtle' \
  --data-binary @shapes/shapes.ttl "$BASE/shacl?graph=$1" | grep -q 'sh:conforms  true' && echo true || echo false; }
# validate a data file against shapes.ttl with the independent Jena CLI; echoes true/false
shacl_cli() { docker compose exec -T jena-tools \
  shacl validate --shapes /shapes/shapes.ttl --data "/data/$1" 2>/dev/null | grep -q 'sh:conforms  true' && echo true || echo false; }

echo "==> bringing up the stack"
docker compose up -d --wait >/dev/null

# ---------------------------------------------------------------- load (idempotent)
loaded=$(q 'SELECT (COUNT(*) AS ?n) WHERE { GRAPH ?g { ?s ?p ?o } }' | tail -1)
if [ "$RESET" = "--reset" ] || [ "${loaded:-0}" = "0" ]; then
  echo "==> loading sample.trig (reset=${RESET:-no})"
  upd 'DROP ALL'
  curl -s -u "$ADMIN" -X POST -H 'Content-Type: application/trig' \
    --data-binary @data/sample.trig "$BASE/data" >/dev/null
else
  echo "==> data already present ($loaded quads) — skipping load (use --reset to rebuild)"
fi

echo; echo "==> CHECK 1 — RDF-star loaded: base edge asserted AND annotation stored"
base=$(q "ASK { GRAPH ?g { <http://gemeten-stad.nl/id/intv-001> <http://gemeten-stad.nl/ns#locatedAt> ?p } }" | tail -1)
ann=$(q "ASK { GRAPH ?g { << <http://gemeten-stad.nl/id/intv-001> <http://gemeten-stad.nl/ns#locatedAt> ?p >> <http://gemeten-stad.nl/ns#confidence> ?c } }" | tail -1)
check "base locatedAt edge asserted" "$base" "true"
check "confidence annotation stored" "$ann" "true"

echo; echo "==> CHECK 2 — SPARQL-star read of << … >> :confidence"
rows=$(q "$(cat sparql/star-read.rq)" | tail -n +2 | grep -c . || true)
conf1=$(q "$(cat sparql/star-read.rq)" | grep intv-001 | cut -d, -f3)
check "star-read returns both edges" "$rows" "2"
check "intv-001 confidence read back" "$conf1" "0.7"

echo; echo "==> CHECK 3 — SHACL accept/reject via the Fuseki endpoint AND the Jena CLI"
check "endpoint: well-formed run graph conforms" "$(shacl_http "$RUN1")" "true"
check "CLI: well-formed conforms"                "$(shacl_cli wellformed.ttl)" "true"
check "CLI: missing structure rejected"          "$(shacl_cli reject-structure.ttl)" "false"
check "CLI: missing confidence rejected"         "$(shacl_cli reject-confidence.ttl)" "false"
# endpoint reject paths (load into temp graphs, validate, drop)
for pair in "urn:t-nostruct reject-structure.ttl" "urn:t-noconf reject-confidence.ttl"; do
  set -- $pair; g="$1"; f="$2"
  curl -s -u "$ADMIN" -X PUT -H 'Content-Type: text/turtle' --data-binary @data/"$f" "$BASE/data?graph=$g" >/dev/null
  check "endpoint: $f rejected" "$(shacl_http "$g")" "false"
  upd "DROP GRAPH <$g>"
done

echo; echo "==> CHECK 4 — named graphs + cross-graph query (data graph ⨝ provenance graph)"
xrows=$(q "$(cat sparql/cross-graph.rq)" | tail -n +2 | grep -c . || true)
check "cross-graph join returns both runs" "$xrows" "2"

echo; echo "==> PROBE A — confidence-presence over RDF-star, and the #3503 scenario"
# a graph that CONTAINS an RDF-star annotation AND a genuine violation: does the
# endpoint still flag exactly the bad node, or does RDF-star presence mask it (#3503)?
mkdir -p out
cat > out/mixed.ttl <<'EOF'
@prefix gs:   <http://gemeten-stad.nl/ns#> .
@prefix data: <http://gemeten-stad.nl/id/> .
data:good a gs:Intervention ; gs:locatedAt data:pg {| gs:confidence 0.8 |} .
data:pg   a gs:Place .
data:bad  a gs:Intervention ; gs:locatedAt data:pb .
data:pb   a gs:Place .
EOF
curl -s -u "$ADMIN" -X PUT -H 'Content-Type: text/turtle' --data-binary @out/mixed.ttl "$BASE/data?graph=urn:t-mixed" >/dev/null
mixed=$(curl -s -u "$ADMIN" -X POST -H 'Content-Type: text/turtle' --data-binary @shapes/shapes.ttl "$BASE/shacl?graph=urn:t-mixed")
echo "$mixed" | grep -q 'sh:conforms  false' && c=false || c=true
echo "$mixed" | grep -q 'data:bad' && focus=bad || focus=none
check "endpoint flags the violation despite RDF-star present (#3503 not triggered)" "$c" "false"
check "endpoint pinpoints the offending node" "$focus" "bad"
upd 'DROP GRAPH <urn:t-mixed>'
echo "  note: confidence-presence is NOT expressible in core SHACL (no path reaches a"
echo "        quoted triple); it requires a sh:sparql SPARQL-star constraint. That"
echo "        constraint works over BOTH the CLI and the endpoint → this is the P8 pattern."

echo; echo "==> PROBE B — run-stamped named-graph ergonomics"
echo "  observed: loading TriG preserves named graphs; each run is its own graph; a"
echo "  cross-graph join to run:_provenance recovers prov:generatedAtTime per run."
echo "  caveat: this dataset runs unionDefaultGraph=on, so run provenance lives in a"
echo "  dedicated named graph rather than the (shadowed) stored default graph."

echo
if [ "$fails" -eq 0 ]; then
  printf '\033[32m==> ALL CHECKS PASS\033[0m — Fuseki satisfies the five needs; see README.md\n'
else
  printf '\033[31m==> %d CHECK(S) FAILED\033[0m\n' "$fails"; exit 1
fi
