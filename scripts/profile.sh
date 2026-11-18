#!/usr/bin/env sh
# Profiles services under steady load:
#   warm-up 2s (no profiles) -> load 30s; at +5s capture allocs and CPU
#   profiles for 20s in parallel, so the window lies inside the steady load.
#
# Usage: scripts/profile.sh <ammo> <service[,service...]> [diff-base-ammo]
#   service: orchestrator | feature-store | candidates-retrieval
#   diff-base-ammo: earlier profiles to compare with (pprof -diff_base)
# Env: RPS (237), CONCURRENCY (16), ORCH_HOST, FS_HOST, CR_HOST,
#      FOCUS (pprof -focus regexp for the console report, optional).
set -e
. "$(dirname "$0")/lib.sh"

[ $# -ge 2 ] || { echo "usage: $0 <ammo> <service[,service...]> [diff-base-ammo]" >&2; exit 1; }
AMMO="$1"
SERVICES="$(echo "$2" | tr ',' ' ')"
BASE="${3:-}"
FILE="$(ammo_file "${AMMO}")"
mkdir -p "${PROFILES_DIR}"

WARMUP=2s
LOAD=30s
PROFILE_DELAY=5
PROFILE_SECONDS=20

echo "==> ${AMMO}: warm-up ${WARMUP} (no profiles)"
fire "${FILE}" "${WARMUP}" "${PROFILES_DIR}/${AMMO}_warmup_ghz.json" > /dev/null

echo "==> ${AMMO}: load ${LOAD}; profiles at +${PROFILE_DELAY}s for ${PROFILE_SECONDS}s: ${SERVICES}"
fire "${FILE}" "${LOAD}" "${PROFILES_DIR}/${AMMO}_ghz.json" &
GHZ_PID=$!

sleep "${PROFILE_DELAY}"
PIDS=""
for svc in ${SERVICES}; do
  url="$(pprof_url "${svc}")"
  curl -fsS -o "${PROFILES_DIR}/${AMMO}_${svc}_allocs.pb.gz" "${url}/debug/pprof/allocs?seconds=${PROFILE_SECONDS}" &
  PIDS="${PIDS} $!"
  curl -fsS -o "${PROFILES_DIR}/${AMMO}_${svc}_cpu.pb.gz" "${url}/debug/pprof/profile?seconds=${PROFILE_SECONDS}" &
  PIDS="${PIDS} $!"
done
for pid in ${PIDS}; do wait "${pid}"; done
wait "${GHZ_PID}"

echo "==> Load results"
report "${PROFILES_DIR}/${AMMO}_ghz.json"

top() { # top <title> <pprof args...>
  title="$1"; shift
  echo
  echo "--- ${title}"
  go tool pprof -top -nodecount=15 ${FOCUS:+-focus="${FOCUS}"} "$@" 2>/dev/null | sed -n '1,25p'
}

for svc in ${SERVICES}; do
  allocs="${PROFILES_DIR}/${AMMO}_${svc}_allocs.pb.gz"
  cpu="${PROFILES_DIR}/${AMMO}_${svc}_cpu.pb.gz"
  echo
  echo "================ ${svc} (${AMMO}) ================"
  top "allocs: objects allocated in the window (alloc_objects)" -sample_index=alloc_objects "${allocs}"
  top "allocs: bytes allocated in the window (alloc_space)" -sample_index=alloc_space "${allocs}"
  top "cpu" "${cpu}"

  if [ -n "${BASE}" ]; then
    base_allocs="${PROFILES_DIR}/${BASE}_${svc}_allocs.pb.gz"
    base_cpu="${PROFILES_DIR}/${BASE}_${svc}_cpu.pb.gz"
    if [ -f "${base_allocs}" ] && [ -f "${base_cpu}" ]; then
      top "diff vs ${BASE}: alloc_objects (negative = fewer allocations)" -sample_index=alloc_objects -diff_base="${base_allocs}" "${allocs}"
      top "diff vs ${BASE}: cpu (negative = less CPU)" -diff_base="${base_cpu}" "${cpu}"
    else
      echo "(no ${BASE} profiles for ${svc} yet: run the ${BASE} profiling step first to get a diff)"
    fi
  fi
done

echo
echo "Profiles saved to ${PROFILES_DIR}/${AMMO}_<service>_{allocs,cpu}.pb.gz"
echo "Web UI: make pprof-ui SVC=<service> AMMO=${AMMO} KIND=<allocs|cpu> [BASE=<ammo>]"
