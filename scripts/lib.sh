# Shared settings and helpers for shoot.sh and profile.sh (sourced, not executed).
# Defaults target the docker-compose service names; override them to run
# against services started on the host, e.g. ORCH_HOST=localhost.

ORCH_HOST="${ORCH_HOST:-orchestrator}"
FS_HOST="${FS_HOST:-feature-store}"
CR_HOST="${CR_HOST:-candidates-retrieval}"
ORCH_ADDR="${ORCH_ADDR:-${ORCH_HOST}:50050}"

# 237, not 250: a request rate in step with the kernel tick rate skews CPU
# profiles (see "Discussion point: why 237 rps" in CHEATSHEET.md).
RPS="${RPS:-237}"
CONCURRENCY="${CONCURRENCY:-16}"
DURATION="${DURATION:-30s}"

METHOD="recsys.v1.RecommendationService/GetRecommendations"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
AMMO_DIR="${ROOT_DIR}/ammo"
PROFILES_DIR="${ROOT_DIR}/profiles"

# ammo_file <name> -> path to ammo/<name>.json
ammo_file() {
  f="${AMMO_DIR}/$1.json"
  if [ ! -f "$f" ]; then
    echo "unknown ammo '$1', available: $(ls "${AMMO_DIR}" | sed 's/\.json$//' | tr '\n' ' ')" >&2
    exit 1
  fi
  echo "$f"
}

# pprof_url <service> -> base URL of the service's pprof/expvar HTTP server
pprof_url() {
  case "$1" in
    orchestrator) echo "http://${ORCH_HOST}:6060" ;;
    candidates-retrieval) echo "http://${CR_HOST}:6061" ;;
    feature-store) echo "http://${FS_HOST}:6062" ;;
    *) echo "unknown service '$1'" >&2; exit 1 ;;
  esac
}

# fire <ammo-file> <duration> <out-json>: runs ghz via server reflection.
fire() {
  ghz --insecure \
    --call "${METHOD}" \
    --data-file "$1" \
    --rps "${RPS}" \
    --concurrency "${CONCURRENCY}" \
    --duration "$2" \
    --duration-stop wait \
    --format json \
    --output "$3" \
    "${ORCH_ADDR}"
}

# report <ghz-json>: prints throughput, latency percentiles and errors.
report() {
  jq -r '
    def ms: . / 1000000 | . * 100 | round / 100;
    def pct(p): ([.latencyDistribution[]? | select(.percentage == p) | .latency] | first // 0) | ms;
    "  requests:   \(.count)  (\(.rps | . * 10 | round / 10) rps)",
    "  latency:    avg \(.average | ms) ms | P50 \(pct(50)) ms | P90 \(pct(90)) ms | P99 \(pct(99)) ms | max \(.slowest | ms) ms",
    "  status:     \(.statusCodeDistribution // {} | to_entries | map("\(.key)=\(.value)") | join(" "))",
    (if (.errorDistribution // {}) != {} then "  errors:     \(.errorDistribution | to_entries | map("\(.value)x \(.key)") | join("; "))" else empty end)
  ' "$1"
}

# vars <service> -> /debug/vars snapshot (response size counters)
vars() {
  curl -fsS "$(pprof_url "$1")/debug/vars"
}

# sizes <before.json> <after.json>: average response wire size per gRPC
# method over the window between two /debug/vars snapshots.
sizes() {
  jq -r -n --slurpfile b "$1" --slurpfile a "$2" '
    ($a[0].grpc_response_bytes // {}) as $ab | ($a[0].grpc_response_count // {}) as $ac |
    ($b[0].grpc_response_bytes // {}) as $bb | ($b[0].grpc_response_count // {}) as $bc |
    $ac | keys[] as $m |
    (($ac[$m] // 0) - ($bc[$m] // 0)) as $n |
    select($n > 0 and ($m | test("^/grpc\\.") | not)) |
    ((($ab[$m] // 0) - ($bb[$m] // 0)) / $n) as $avg |
    "  \($m | sub("^/"; "") | .[0:68] | . + (" " * (68 - length)))  avg \(if $avg >= 1024 then "\($avg / 1024 | . * 10 | round / 10) KiB" else "\($avg | round) B" end)"
  '
}
