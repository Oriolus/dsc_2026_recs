# Data-Oriented Design for Backend Systems: Squeezing 100% CPU Efficiency

Welcome to the companion repository for the **"Data-Oriented Design for Backend Systems: Squeezing 100% CPU Efficiency"** technical workshop!

In this 90-minute hands-on tutorial, we explore why high-load recommendation systems often hit a performance wall not in machine learning inference, but in the network transport and serialization layer. Using a live multi-service Go sandbox, we will diagnose memory allocation bottlenecks under load and iteratively refactor communication contracts using Data-Oriented Design (DoD) principles to **drastically reduce heap allocations** on the hot path, compress network payloads, and maximize CPU efficiency.

---

## 1. Workshop Overview: Problems & Engineering Goals

### The Problem

In distributed recommendation systems (RecSys), microservice communication is traditionally designed around idiomatic object hierarchies, domain encapsulation, and standard Protobuf code generation (`protoc-gen-go`).

While this works at modest scale, expanding candidate pools and feature sets per request creates an allocation storm:

1. **Feature Store Bottleneck:** Polymorphic feature wrappers (`oneof` messages modeled after standard Feast serving contracts) instantiate tens of thousands of short-lived heap objects per single request. Serialization and `runtime.mallocgc` quickly become dominant consumers of CPU time.
2. **Fat Candidates Bottleneck:** Enriching candidates with product metadata and advertising bids at the `CandidatesRetrieval` stage (`[]*Candidate`) allocates thousands of nested pointers per batch, degrading orchestrator throughput.
3. **Naive Pooling Contention:** Enabling out-of-the-box per-struct object pooling (`sync.Pool`) on thousands of nested objects triggers severe cross-core lock contention (pool-stealing) and `protoimpl` inspection overhead.

### What We Will Do

You do **not** need to write boilerplate code from scratch during the session. All four optimization stages are pre-compiled into the sandbox and can be toggled dynamically. Together, we will:

* **Generate realistic gRPC traffic** against the pipeline and observe tail latency degradation under load.
* **Capture and dissect CPU & Memory profiles** (`pprof` flame graphs) to understand how Go's garbage collector and memory allocator behave on pointer-heavy graphs.
* **Refactor the Feature Store contract** into a contiguous **1D Flat Stride Matrix** (`[]float32`) with schema descriptors and IEEE-754 `NaN` representation for missing values.
* **Re-architect the `CandidatesRetrieval` $\to$ `Orchestrator` contract** using **Parallel Arrays (hybrid SoA)**, **Batch-Level Pooling**, and custom in-place slice capacity reuse (`CustomResetVT`) in `vtprotobuf`.
* **Stress-test memory safety** by reproducing a `Use-After-Reset` data race and implementing anomaly protection (`maxRetainedCap`) for `sync.Pool`.

---

## 2. Recommendation System Architecture & Call Pipeline

The sandbox emulates a two-stage recommendation runtime composed of three core Go microservices (`Orchestrator`, `CandidatesRetrieval`, and `FeatureStore`) and a load-testing/profiling `toolbox` container:

```text
                       ┌──────────────────────┐
                       │   Incoming Request   │
                       │ (GetRecommendations) │
                       └──────────┬───────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ Orchestrator [:50050 | pprof :6060]                                     │
│                                                                         │
│  ┌────────────────────┐    1. Fetch Candidates    ┌───────────────────┐ │
│  │ 1. candidates      ├──────────────────────────►│CandidatesRetrieval│ │
│  └─────────┬──────────┘    (Thin IDs or Fat)      │     [:50051]      │ │
│            │                                      └───────────────────┘ │
│            ▼                                                            │
│  ┌────────────────────┐    2. Hydrate Features    ┌───────────────────┐ │
│  │ 2. scoring         ├──────────────────────────►│   FeatureStore    │ │
│  └─────────┬──────────┘    (Oneof or Flat Matrix) │     [:50052]      │ │
│            │                                      └───────────────────┘ │
│            ▼                                                            │
│  ┌────────────────────┐                                                 │
│  │ 3. ranking         │  (Combine retrieval score + ML features + bids) │
│  └─────────┬──────────┘                                                 │
│            │                                                            │
│            ▼                                                            │
│  ┌────────────────────┐                                                 │
│  │ 4. post-processing │  (Brand/Seller diversity & Top-K cut)           │
│  └────────────────────┘                                                 │
└─────────────────────────────────────────────────────────────────────────┘
```

### The 3 Microservices

1. **`orchestrator` (`RecommendationService`):** The entry point (`GetRecommendations`). Coordinates candidate retrieval, feature hydration, scoring, ranking, and diversity filtering.
2. **`candidates-retrieval` (`CandidatesRetrievalService`):** Fast primary candidate generator. Supports returning either lightweight candidate IDs (`[]uint64` — *Pull model*) or metadata-enriched candidates containing seller, category, brand, and ad bid structs (*Push / "Fat Candidates" model*).
3. **`feature-store` (`FeatureStoreService`):** Online feature serving service. Delivers numeric and vector features for each of the requested candidate IDs.

### Request Execution Pipeline inside the Orchestrator

Every `GetRecommendations` call executes four sequential stages:

1. **`candidates`:** Requests $N$ candidate items from `CandidatesRetrieval` (starting with a small pool at baseline and scaling up to a heavy batch).
2. **`scoring`:** Passes the $N$ candidate IDs and $M$ feature names to `FeatureStore`, unpacks the feature payload, and evaluates candidate scores (gracefully skipping missing features encoded as `math.NaN()`).
3. **`ranking`:** Combines hydrated feature scores with `retrieval_score` and advertising bids (`cpc_bid`, `cpm_bid`).
4. **`post-processing`:** Applies business diversity rules (deduplicating items from identical sellers/brands) and returns the sorted `RecommendResponse`.

---

## 3. Technology Stack & Tooling

All required tools are pre-installed inside the Docker environment (`toolbox` container) and configured for GitHub Codespaces:

* **Language & Runtime:** **Go 1.22+** (utilizing standard concurrency primitives, `sync.Pool`, `net/http/pprof`, and `testing.B`).
* **RPC & IDL:** **gRPC-Go** (`google.golang.org/grpc`) and **Protocol Buffers v3** (`google.golang.org/protobuf`).
* **Schema Management & Codegen:** **Buf** (`buf.build`) managing dual compilation pipelines (`buf.gen.yaml` and `buf.gen.vtpool.yaml`).
* **Allocation-Efficient Serialization:** **`planetscale/vtprotobuf`** (`protoc-gen-go-vtproto`) generating reflection-free marshalers, unmarshalers (`UnmarshalVT`), and struct reuse helpers.
* **Load Generation:** **`ghz`** — CLI gRPC load generator used to fire controlled request rates and print latency histograms and percentiles ($P50 / P90 / P99$) directly in the terminal.
* **Profiling & Benchmarking:** **`go tool pprof`** (interactive Flame Graphs for `-alloc_objects`, `-alloc_space`, and CPU) and **`benchstat`** for statistical benchmark comparisons.
* **Infrastructure:** **Docker & Docker Compose** / **GitHub Codespaces**.

---

## 4. Workshop Timeline & Stages (90 Minutes)

Below is the step-by-step roadmap of what we will cover during the session, along with the commands used at each stage.

### Stage 0: Environment Spin-Up & Verification (00:00 – 00:07 | 7 mins)

* **Goal:** Start the 3 microservices and verify network connectivity.
* **What we do:** Spin up the Docker Compose cluster and check service health endpoints.
* **Commands:**
  ```bash
  make up
  make check
  ```

### Stage 1: System Context & Small-Pool Baseline (00:07 – 00:22 | 15 mins)

* **Problem Investigated:** How a two-stage RecSys pipeline operates with thin candidates (`[]uint64`) and a Feast-style polymorphic feature contract (`oneof val`) at modest scale.
* **What we dissect:**
  * Review `api/proto/recsys/v1/feature_store.proto` (`GetFeaturesResponse` with nested `EntityFeatures` and `FeatureValue`).
  * Run a baseline load test on a small candidate pool and observe how available CPU headroom masks underlying serialization inefficiencies.
* **Commands:**
  ```bash
  make test-v1-small
  ```

### Stage 2: Scaling the Feature Store — `oneof` Explosion vs. 1D Flat Stride Matrix (00:22 – 00:44 | 22 mins)

* **Problem Investigated:** Expanding the candidate pool and feature set creates a massive burst of nested heap allocations per request, causing `runtime.mallocgc` and GC pointer scanning to dominate CPU profiles and inflate tail latency.
* **What we dissect:**
  * Fire heavy traffic at Iteration 1 (`PIPELINE_MODE_V1_FEAST_ONEOF`) and inspect the `pprof` Flame Graph.
  * Discuss why **Apache Arrow Flight** was ruled out due to platform governance and CI/CD standardization on Protobuf/Buf.
  * Examine Iteration 2 (`PIPELINE_MODE_V2_FLAT_STRIDE`): packing the 2D feature matrix into a contiguous 1D `repeated float values` slice accompanied by compact `FeatureDescriptor` headers (`name`, `data_type`, `offset`, `length`).
  * Understand why Go marks primitive `[]float32` backing arrays as `noscan` (invisible to GC traversal) and how `math.NaN()` represents missing values without presence bitmasks.
* **Commands:**
  ```bash
  make test-v1-heavy
  make profile-v1-fs

  make test-v2-flat
  make profile-v2-fs
  ```

### Stage 3: Moving to "Fat Candidates" (`CandidatesRetrieval` $\to$ `Orchestrator`) & The `sync.Pool` Trap (00:44 – 01:04 | 20 mins)

* **Problem Investigated:** Downstream business logic requires product metadata (`Metadata`) and ad bids (`AdvBids`) for brand/merchant diversity. Switching `CandidatesRetrieval` $\to$ `Orchestrator` to a classic Array-of-Structs (`repeated Candidate` $\to$ `[]*Candidate`) causes thousands of heap allocations per request. Furthermore, enabling standard `vtprotobuf` object pooling (`--go-vtproto_opt=pool=...`) hits a wall due to multi-core `sync.Pool` contention.
* **What we dissect:**
  * Why Go's Protobuf compiler always generates a slice of pointers (`[]*T`) instead of value slices (`[]T`) due to internal `protoimpl.MessageState` locks.
  * Benchmark and profile Iteration 3a (`PIPELINE_MODE_V3_FAT_NAIVE`).
  * Benchmark and profile Iteration 3b (`PIPELINE_MODE_V3_FAT_VTPOOL`) and observe lock contention across processor-local pools (`P`) and `protoimpl.X.MessageStateOf` overhead when pooling thousands of small structs individually.
* **Commands:**
  ```bash
  make test-v3-naive
  make profile-v3-naive

  make test-v3-vtpool
  make profile-v3-vtpool
  ```

### Stage 4: Parallel Arrays, Batch-Level Pooling & `CustomResetVT` (01:04 – 01:20 | 16 mins)

* **Problem Investigated:** How to eliminate per-item struct allocations in `CandidatesRetrieval` $\to$ `Orchestrator` deserialization and avoid `sync.Pool` contention without flattening dozens of nested fields into an unmaintainable pure Structure-of-Arrays (SoA) schema.
* **What we dissect:**
  * Hybrid Parallel Arrays in `CandidateBatchResponse` (`ids`, `retrieval_scores`, `metadata`, `adv_bids`).
  * Go slice header mechanics (`ptr`, `len`, `cap`): how `vtprotobuf`'s `UnmarshalVT` automatically reuses non-nil pointers beyond `len` up to `cap`.
  * Inspect `internal/pooling/custom_reset.go`: pooling only the root `CandidateBatchResponse` object, zeroing child structs in-place via `FastReset()`, and truncating slice lengths (`x.Metadata = x.Metadata[:0]`) while keeping backing pointers intact.
* **Commands:**
  ```bash
  make test-v4-pooled
  make profile-v4-pooled
  make bench-compare
  ```

### Stage 5: Engineering Risks, Safety Guards & Production Results (01:20 – 01:30 | 10 mins)

* **Problem Investigated:** Manual memory reuse introduces C/C++-style lifecycle hazards: pointer leaks across goroutines (`Use-After-Reset`), permanent memory bloat in `sync.Pool` after anomalous traffic spikes, and parallel slice length mismatches.
* **What we dissect:**
  * Trigger a live `DATA RACE` (`-race`) when an asynchronous logger retains a pooled `*Metadata` pointer after the request finishes.
  * Review the three production safeguards: strict single-ownership rules (copying primitives), upper capacity clamping (`maxRetainedCap` in `PutBatch`), and slice boundary validation.
  * Review real-world production impact: scaling traffic 3x while cutting normalized CPU burn by more than 55%.
* **Commands:**
  ```bash
  make test-race
  make test-anomaly-cap
  ```

---

## 5. Ports & Repository Navigation

### Exposed Local Ports

| Service / Tool | Port | Description |
| :--- | :--- | :--- |
| **`orchestrator` (gRPC)** | `:50050` | Main entry point (`RecommendationService/GetRecommendations`) |
| **`candidates-retrieval` (gRPC)** | `:50051` | Candidate retrieval service (`CandidatesRetrievalService`) |
| **`feature-store` (gRPC)** | `:50052` | Online feature store service (`FeatureStoreService`) |
| **`orchestrator` (`pprof`)** | `:6060` | Raw `net/http/pprof` endpoint for the Orchestrator |
| **`feature-store` (`pprof`)** | `:6062` | Raw `net/http/pprof` endpoint for the Feature Store |
| **Interactive `pprof` Web UI (Orchestrator)** | `:8080` | Browser Flame Graph UI for Orchestrator profiles |
| **Interactive `pprof` Web UI (FeatureStore)** | `:8081` | Browser Flame Graph UI for Feature Store profiles |

### Key Files to Explore

* `api/proto/recsys/v1/` — Protobuf definitions for all four iterations.
* `buf.gen.yaml` & `buf.gen.vtpool.yaml` — Buf code generation pipelines for `vtprotobuf`.
* `internal/features/accessor.go` — Offset arithmetic (`candidateIdx*stride + offset`) and `math.NaN()` handling.
* `internal/orchestrator/mode_v*.go` — Side-by-side implementations of all pipeline modes.
* `internal/pooling/custom_reset.go` — Root batch `sync.Pool`, `CustomResetBatch()`, and `maxRetainedCap` guard.
* `internal/safety/race_demo_test.go` — Reproducible `Use-After-Reset` data race and safe primitive copy patterns.
* `CHEATSHEET.md` — Quick one-page command & metric reference for the live session.
