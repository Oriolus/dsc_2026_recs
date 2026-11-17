#!/usr/bin/env sh
# Load test of the orchestrator with ghz.
# Usage: scripts/shoot.sh <ammo>   (e.g. v1_small, v2_flat, v4_pooled)
# Env: RPS (250), CONCURRENCY (16), DURATION (30s), ORCH_HOST (orchestrator).
set -e
. "$(dirname "$0")/lib.sh"

[ $# -eq 1 ] || { echo "usage: $0 <ammo>" >&2; exit 1; }
AMMO="$1"
FILE="$(ammo_file "${AMMO}")"
mkdir -p "${PROFILES_DIR}"
OUT="${PROFILES_DIR}/${AMMO}_ghz.json"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

echo "==> ${AMMO}: $(cat "${FILE}")"
echo "    ghz --rps ${RPS} --concurrency ${CONCURRENCY} --duration ${DURATION} -> ${ORCH_ADDR}"

vars orchestrator > "${TMP}/before.json"
fire "${FILE}" "${DURATION}" "${OUT}"
vars orchestrator > "${TMP}/after.json"

echo "==> Results (${OUT})"
report "${OUT}"
echo "==> Average gRPC response size (wire bytes, seen by the orchestrator)"
sizes "${TMP}/before.json" "${TMP}/after.json"
