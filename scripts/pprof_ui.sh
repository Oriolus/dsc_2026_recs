#!/usr/bin/env sh
# Opens a saved profile in the pprof web UI (inside the toolbox container).
# Usage: scripts/pprof_ui.sh <service> <ammo> <allocs|cpu> [diff-base-ammo]
# Orchestrator (and candidates-retrieval) -> :8080, Feature Store -> :8081.
set -e
. "$(dirname "$0")/lib.sh"

[ $# -ge 3 ] || { echo "usage: $0 <service> <ammo> <allocs|cpu> [diff-base-ammo]" >&2; exit 1; }
SVC="$1"; AMMO="$2"; KIND="$3"; BASE="${4:-}"

PORT=8080
[ "${SVC}" = "feature-store" ] && PORT=8081

PROFILE="${PROFILES_DIR}/${AMMO}_${SVC}_${KIND}.pb.gz"
[ -f "${PROFILE}" ] || { echo "no profile ${PROFILE}; run the matching 'make profile-*' first" >&2; exit 1; }

SAMPLE=""
[ "${KIND}" = "allocs" ] && SAMPLE="-sample_index=alloc_objects"
DIFF=""
[ -n "${BASE}" ] && DIFF="-diff_base=${PROFILES_DIR}/${BASE}_${SVC}_${KIND}.pb.gz"

echo "pprof UI for ${PROFILE} ${DIFF} -> http://localhost:${PORT}"
exec go tool pprof -http="0.0.0.0:${PORT}" -no_browser ${SAMPLE} ${DIFF} "${PROFILE}"
