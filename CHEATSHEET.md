# Workshop Cheatsheet: High-Throughput RecSys Runtime in Go

You do not write code today. You run load scenarios (`ghz`), take CPU and memory
profiles (`pprof`), compare microbenchmarks (`benchstat`) and read the code of four
iterations of network contracts and memory handling.

## Quick start

First-time Docker setup on macOS (Colima) and Windows (WSL 2): see [SETUP.md](SETUP.md).

```sh
make up      # build the image and start candidates-retrieval, feature-store, orchestrator, toolbox
make check   # gRPC health of all services + an end-to-end smoke call
make help    # list all commands
make down    # stop everything (including toolbox)
```

All load and profiling commands run inside the `toolbox` container
(`docker compose exec toolbox ...`), which has `ghz`, `grpcurl`, `go tool pprof`,
`graphviz`, `benchstat` and `buf`. Profiles are written to `./profiles` on your machine.

## Services and ports

| Service | gRPC | pprof / expvar | pprof web UI (from toolbox) |
|---|---|---|---|
| `orchestrator` (`RecommendationService`) | `localhost:50050` | http://localhost:6060/debug/pprof/ | http://localhost:8080 |
| `candidates-retrieval` | `localhost:50051` | http://localhost:6061/debug/pprof/ | http://localhost:8080 |
| `feature-store` | `localhost:50052` | http://localhost:6062/debug/pprof/ | http://localhost:8081 |

`/debug/vars` on the pprof port exposes `grpc_response_bytes` / `grpc_response_count`
per gRPC method; `shoot.sh` uses them to report average response sizes.

All gRPC servers register Health Check and Server Reflection, so no `.proto` files are needed:

```sh
docker compose exec toolbox grpcurl -plaintext orchestrator:50050 list
docker compose exec toolbox grpcurl -plaintext feature-store:50052 describe recsys.v1.FlatFeaturesResponse
docker compose exec toolbox grpcurl -plaintext \
  -d '{"user_id": 42, "num_candidates": 5, "num_features": 4, "mode": "PIPELINE_MODE_V4_FAT_POOLED"}' \
  orchestrator:50050 recsys.v1.RecommendationService/GetRecommendations
docker compose exec toolbox grpcurl -plaintext \
  -d '{"entity_ids": [1, 2], "feature_names": ["f_0", "f_1", "f_2"]}' \
  feature-store:50052 recsys.v1.FeatureStoreService/GetFeaturesV2Flat
```

## Commands

| Block | Command | What it does |
|---|---|---|
| 0 | `make up` / `make check` / `make down` | Start, verify, stop the stand |
| 1 | `make test-v1-small` | v1 on the small pool (N=300, M=40): fits P99 <= 100 ms |
| 2 | `make test-v1-heavy` | v1 with N=1000, M=120: N*M `FeatureValue` objects per request, SLA breach |
| 2 | `make profile-v1-fs` | Allocation + CPU profiles of Feature Store and Orchestrator for v1 |
| 2 | `make test-v2-flat` | v2: one flat `[]float32` + M `FeatureDescriptor`s, NaN = missing |
| 2 | `make profile-v2-fs` | Profiles for v2 + `-diff_base` against v1 |
| 3 | `make test-v3-naive` | v3a: fat AoS candidates, `UnmarshalVT` into a fresh message, no pool |
| 3 | `make profile-v3-naive` | Profiles focused on decoding/cleanup of the Candidates Retrieval response |
| 3 | `make test-v3-vtpool` | v3b: vtprotobuf built-in pool out of the box |
| 3 | `make profile-v3-vtpool` | Profiles + diff against v3a |
| 4 | `make test-v4-pooled` | v4: parallel arrays + one batch `sync.Pool` + manual reset |
| 4 | `make profile-v4-pooled` | Profiles after a 2 s warm-up + diff against v3b |
| 4 | `make bench-compare` | `testing.B` microbenchmarks summarized by `benchstat` |
| 5 | `make test-anomaly-cap` | Unit test: a batch with `cap > maxRetainedCap (1500)` is never pooled |
| - | `make pprof-ui SVC=... AMMO=... KIND=allocs\|cpu [BASE=...]` | pprof web UI for a saved profile |

Load parameters (env of `scripts/shoot.sh` / `scripts/profile.sh`): `RPS=250`,
`CONCURRENCY=16`, `DURATION=30s`. Override them, for example:
`docker compose exec -e RPS=500 toolbox ./scripts/shoot.sh v2_flat`.

### How profiling works (`scripts/profile.sh`)

1. Warm-up: 2 s of load without profiles (pools and slices reach `cap=1000`).
2. Load: 30 s. At +5 s, `allocs?seconds=20` and `profile?seconds=20` (CPU) are captured
   in parallel, so the 20 s window lies entirely inside the steady load.
3. The console shows `pprof -top` for `alloc_objects`, `alloc_space` and CPU, and a
   `-diff_base` against the previous iteration (negative = better).

Files: `profiles/<ammo>_<service>_{allocs,cpu}.pb.gz`, `profiles/<ammo>_ghz.json`.

Blocks 3-4 use `FOCUS='fetchFatCandidates|ReturnToVTPool|ResetVT|FromVTPool|PutBatch|CustomResetBatch|GetBatch'`,
i.e. only the code that receives, decodes and cleans up the Candidates Retrieval response.
The same `fetchFatCandidates*` wrapper exists in all three fat modes, so the comparison is fair.

### pprof web UI

```sh
make pprof-ui SVC=feature-store AMMO=v1_heavy KIND=allocs             # http://localhost:8081
make pprof-ui SVC=orchestrator AMMO=v3_vtpool KIND=cpu                # http://localhost:8080
make pprof-ui SVC=orchestrator AMMO=v4_pooled KIND=cpu BASE=v3_vtpool # diff view
```

Useful views: *Flame Graph*, *Top*, *Source*. In the UI, put the focus regexp above into
the search box to narrow it to the Candidates Retrieval path.

Raw `go tool pprof` inside toolbox:

```sh
docker compose exec toolbox go tool pprof -top -sample_index=alloc_objects profiles/v1_heavy_feature-store_allocs.pb.gz
docker compose exec toolbox go tool pprof -top -diff_base=profiles/v3_naive_orchestrator_allocs.pb.gz \
  -sample_index=alloc_objects profiles/v3_vtpool_orchestrator_allocs.pb.gz
```

## What to look at, block by block

The orchestrator decodes every upstream response with the generated `UnmarshalVT`
(`codec.VTProto`), never with the reflection-based `proto.Unmarshal`. Iterations differ
only in contracts and pooling, and allocation profiles point at concrete generated
lines. Use `-lines` (or the *Source* view in the web UI) to see which object is created:

```sh
go tool pprof -top -lines -sample_index=alloc_objects profiles/v1_heavy_orchestrator_allocs.pb.gz
```

### Block 1-2: Feature Store, oneof vs flat stride

* Schema (identical for v1 and v2, depends only on M): `f_0` is a vector of 10 floats,
  `f_1` a vector of 8, `f_2 ... f_{M-1}` are scalars; `stride = M + 16`.
* v1 (`GetFeaturesV1`): every value is a `*FeatureValue` with a oneof wrapper, plus an
  `EntityFeatures` per candidate and a `FloatVector` per vector feature, i.e. on the order
  of N*M objects on the server (marshal) and again on the orchestrator (unmarshal).
  Look for `features.(*Store).GetFeaturesV1` in the Feature Store allocs profile (and
  `impl.(*MessageInfo).marshalAppendPointer` in its CPU profile: servers still marshal
  with the standard codec). In the orchestrator, `-lines` shows the generated lines in
  `pkg/pb/feature_store_vtproto.pb.go`:

  | Function | Generated line | Objects per response |
  |---|---|---|
  | `(*EntityFeatures).UnmarshalVT` | `m.Features = append(m.Features, &FeatureValue{})` | N*M = 120 000 |
  | `(*FeatureValue).UnmarshalVT` | `m.Val = &FeatureValue_FloatVal{...}` (oneof wrapper in an interface) | ~118 000 |
  | `(*FeatureValue).UnmarshalVT` | `v := &FloatVector{}`, `m.Val = &FeatureValue_VectorVal{...}` | 2 * 2 000 |
  | `(*GetFeaturesResponse).UnmarshalVT` | `append(m.Entities, &EntityFeatures{})` | 1 000 |

  A oneof costs two allocations per scalar: the message and the wrapper stored in the
  interface field.
* Not the codec's fault: `proto.Unmarshal` creates exactly the same ~250 000 objects per
  response, only through `reflect.New` (shown as `reflect.unsafe_New` in profiles), and
  decodes about 2x slower. Compare `BenchmarkUnmarshal_AoS_ProtoReflect` and
  `BenchmarkUnmarshal_AoS_Naive` in `make bench-compare`.
* v2 (`GetFeaturesV2Flat`): one `[]float32` of `N*stride` values plus M descriptors.
  Read with `features.GetScalarFeature` / `GetVectorFeature` (`internal/features/accessor.go`).
* `shoot.sh` prints average wire sizes: v1 is ~1.7x larger on the wire than v2 for N=1000, M=120.

### Block 3: fat candidates, naive vs the vtprotobuf pool

* v3a: `UnmarshalVT` into a fresh `pb.RetrievalResponse`, no pool: a `Candidate`, a
  `Metadata` and an `AdvBids` per candidate (~3000 objects per response). With `-lines`
  (`pkg/pb/candidates_retrieval_vtproto.pb.go`): `m.Candidates = append(m.Candidates, &Candidate{})`,
  `m.Metadata = &Metadata{}`, `m.AdvBids = &AdvBids{}` under `fetchFatCandidatesNaive`.
* v3b decodes into `pbpool.RetrievalResponseFromVTPool()` through
  `conn.Invoke(..., grpc.ForceCodec(codec.VTProto{}))` and returns it with
  `defer resp.ReturnToVTPool()`. The custom codec matters doubly here: the default gRPC
  codec calls `proto.Unmarshal`, which resets the message first and drops every
  warmed-up pointer.

Generated calls of v3b you will see in the CPU profile (`pkg/pbpool/candidates_retrieval_vtproto.pb.go`):

| Generated function | What it does per request (N=1000) |
|---|---|
| `(*RetrievalResponse).ReturnToVTPool` → `ResetVT` | loops over all candidates, calls `Candidate.ResetVT`, then `m.Reset()` |
| `(*Candidate).ResetVT` | `m.Metadata.ReturnToVTPool()`, `m.AdvBids.ReturnToVTPool()`, then `m.Reset()` |
| `(*Metadata).ReturnToVTPool` / `(*AdvBids).ReturnToVTPool` | `ResetVT()` → `m.Reset()`, then `sync.Pool.Put` (1000 + 1000 `Put`s) |
| `(*Metadata).Reset` / `(*AdvBids).Reset` / `(*Candidate).Reset` (in `*.pb.go`) | `*x = T{}` + `protoimpl.X.MessageStateOf(...)` + `ms.StoreMessageInfo(mi)` for each of the 3000 objects |
| `(*Candidate).UnmarshalVT` → `MetadataFromVTPool` / `AdvBidsFromVTPool` | `m.Metadata` is nil after `Reset`, so every candidate does a `sync.Pool.Get` (1000 + 1000 `Get`s) |
| `sync.(*Pool).Put` / `Get` / `pin`, `poolChain.pushHead` / `popTail` | the cost of 4000 `Get`/`Put` calls per request |

Allocations in `UnmarshalVT` almost disappear; CPU now goes to `Reset` with `protoimpl`
bookkeeping and per-object `Get`/`Put` of the nested messages. In the microbenchmark
v3b is barely faster than v3a: the built-in pool trades allocations for cleanup work
of about the same cost.

**Careful with focused CPU profiles.** With the focus regexp, v3a looks *cheaper* in CPU
than v3b: an allocation is cheap at the call site, its real cost is paid later by the
GC in background goroutines (`runtime.gcBgMarkWorker`), outside the focused stacks.
Compare the whole process too:

```sh
go tool pprof -top -focus='gcBgMarkWorker|gcAssist|mallocgc' profiles/v3_naive_orchestrator_cpu.pb.gz
```

### Block 4: one batch pool with manual reset

* `CandidateBatchResponse` is a set of parallel arrays: `Ids`, `RetrievalScores`,
  `[]*Metadata`, `[]*AdvBids`.
* `pooling.GetBatch` / `PutBatch` use one `sync.Pool` for the whole batch.
  `CustomResetBatch` zeroes public fields of every `Metadata`/`AdvBids` inside `cap` in
  place and truncates the slices to `[:0]`. The generated `UnmarshalVT` then reuses the
  pointers it finds within `cap`: no `Get`/`Put` per candidate, no `Reset()`/`protoimpl`.
* Why not `*m = pbpool.Metadata{}`? It wipes `protoimpl.MessageState`, and the protobuf
  runtime would re-initialize it later: the same work as the generated `Reset()`.
* Rule: never sort `batch.Metadata` / `batch.AdvBids` in place and never overwrite
  pointers inside `batch` (unique pointers in the backing array are the pool invariant).
  The orchestrator sorts and filters only its own `items` slice.

### Block 5: anomaly protection

`PutBatch` drops batches with `cap(Ids) > maxRetainedCap (1500)`, so one anomalous
request cannot pin huge buffers in the pool forever. `make test-anomaly-cap`.

## Reference numbers (example run, not targets)

Microbenchmarks, N=1000 candidates, one full cycle "take from pool → `UnmarshalVT` → return/reset"
(Apple M-series, 8 cores):

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `Unmarshal_AoS_ProtoReflect` (`proto.Unmarshal`, reference) | ~268 000 | ~241 600 | 3012 |
| `Unmarshal_AoS_Naive` (`UnmarshalVT`, no pool) | ~127 000 | ~241 500 | 3011 |
| `Unmarshal_AoS_VTPool` (`ReturnToVTPool`) | ~120 000 | ~45 | 0 |
| `Unmarshal_BatchPool_CustomReset` (`GetBatch`/`PutBatch`) | ~62 000 | ~10 | 0 |

End-to-end (same machine, services running natively, 250 rps, 20 s profile window):

Blocks 1-2 (no focus, whole process):

| Mode | P50 / P99 | Feature Store: objects allocated | Orchestrator: objects allocated |
|---|---|---|---|
| `v1_small` | ~3 ms / ~11 ms | - | - |
| `v1_heavy` | ~88 ms / ~180 ms, holds only ~175 of 250 rps | ~527 M | ~560 M |
| `v2_flat` | ~2.8 ms / ~4 ms | ~1.2 M | ~7 M |

Blocks 3-4 (orchestrator; "focus" = the Candidates Retrieval path, see `FOCUS` above;
"GC + malloc" = whole process):

| Mode | P50 / P99 | objects allocated in focus | CPU in focus | CPU in GC + malloc |
|---|---|---|---|---|
| `v3_naive` | ~2.9 ms / ~3.8 ms | ~15.0 M | ~0.27 s | ~1.32 s |
| `v3_vtpool` | ~2.9 ms / ~3.9 ms | ~1.6 M | ~0.46 s | ~1.16 s |
| `v4_pooled` | ~2.7 ms / ~5 ms | ~2.9 M | ~0.21 s | ~0.89 s |

Your numbers will differ (Docker, CPU, OS). What matters is the direction between iterations.

### Discussion point: `sync.Pool` vs the GC

In the microbenchmark v4 is at 0 allocs/op, but under end-to-end load `UnmarshalVT` in
v4 still allocates: regularly a request gets a fresh, empty batch from `New`.
Reason: `sync.Pool` is cleared on every GC cycle (objects survive one more cycle in the
victim cache). The orchestrator's live heap is tiny (~7 MB), so with `GOGC=100` the GC
goal is ~15 MB, and the ~0.5 MiB per request of the flat feature response alone triggers
a GC roughly every 20 ms at 250 rps (`GODEBUG=gctrace=1`), comparable to how often each
P sees a request. A pool is only as warm as the GC lets it be: check the GC rate before
trusting a pool, and look at what else allocates on the same hot path.

## Running without Docker

```sh
go run ./cmd/candidates-retrieval &
go run ./cmd/feature-store &
go run ./cmd/orchestrator &
ORCH_HOST=localhost FS_HOST=localhost CR_HOST=localhost ./scripts/shoot.sh v2_flat
ORCH_HOST=localhost FS_HOST=localhost CR_HOST=localhost ./scripts/profile.sh v1_heavy feature-store,orchestrator
go test -run='^$' -bench=. -benchmem ./internal/pooling/
```

Requires `ghz`, `grpcurl`, `jq`, `curl` and `graphviz` locally.

## Code map

| Path | What |
|---|---|
| `api/proto/recsys/v1/*.proto` | Contracts: recommendation API, Feature Store v1/v2, Candidates Retrieval |
| `api/proto/recsys/pool/v1/candidates_retrieval.proto` | Wire-compatible copy in `recsys.pool.v1` for pooled Go types |
| `pkg/pb`, `pkg/pbpool` | Generated code (`scripts/gen_proto.sh` = `buf lint` + `buf generate`) |
| `pkg/pbpool/wire_compat_test.go` | Guards the two copies against drifting apart |
| `internal/features` | Feature Store and flat-stride accessors |
| `internal/candidates` | Candidates Retrieval (thin, fat AoS, fat batch) |
| `internal/orchestrator/mode_*.go` | The five pipeline iterations |
| `internal/codec/vtproto.go` | gRPC codec calling `MarshalVT`/`UnmarshalVT` without reset |
| `internal/pooling` | Batch pool, manual reset, microbenchmarks |
