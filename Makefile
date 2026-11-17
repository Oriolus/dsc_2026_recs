# Prefer the "docker compose" plugin, fall back to standalone docker-compose.
COMPOSE ?= $(shell docker compose version >/dev/null 2>&1 && echo "docker compose" || echo docker-compose)
TOOLBOX := $(COMPOSE) exec -T toolbox

# pprof -focus for Blocks 3-4: decoding and cleanup of the Candidates Retrieval response.
FOCUS_CR := fetchFatCandidates|ReturnToVTPool|ResetVT|FromVTPool|PutBatch|CustomResetBatch|GetBatch
TOOLBOX_CR := $(COMPOSE) exec -T -e FOCUS='$(FOCUS_CR)' toolbox

# pprof web UI parameters: make pprof-ui SVC=orchestrator AMMO=v4_pooled KIND=cpu [BASE=v3_vtpool]
SVC ?= orchestrator
AMMO ?= v4_pooled
KIND ?= cpu
BASE ?=

.PHONY: help up check down logs \
	test-v1-small test-v1-heavy profile-v1-fs test-v2-flat profile-v2-fs \
	test-v3-naive profile-v3-naive test-v3-vtpool profile-v3-vtpool \
	test-v4-pooled profile-v4-pooled bench-compare test-anomaly-cap \
	pprof-ui gen test clean-profiles

help: ## Show available commands
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# --- Block 0: Stand management ---
up: ## Build and start all containers in the background
	$(COMPOSE) up -d --build --wait

check: ## Health Check of all three services + smoke call through the orchestrator
	$(TOOLBOX) ./scripts/check.sh

down: ## Stop the stand, including toolbox
	$(COMPOSE) down

logs: ## Follow service logs
	$(COMPOSE) logs -f orchestrator candidates-retrieval feature-store

# --- Block 1: Exposition and small pool (N=300, M=40) ---
test-v1-small: ## Load v1 on the small pool (fits P99 <= 100ms)
	$(TOOLBOX) ./scripts/shoot.sh v1_small

# --- Block 2: Feature Store explosion (N=1000, M=120) and Flat Stride Matrix ---
test-v1-heavy: ## Load v1_feast_oneof (N*M FeatureValue objects per request, SLA breach)
	$(TOOLBOX) ./scripts/shoot.sh v1_heavy

profile-v1-fs: ## Allocation/CPU profiles of Feature Store and Orchestrator for v1
	$(TOOLBOX) ./scripts/profile.sh v1_heavy feature-store,orchestrator

test-v2-flat: ## Load v2_flat_stride (1D float32 array + FeatureDescriptor + NaN)
	$(TOOLBOX) ./scripts/shoot.sh v2_flat

profile-v2-fs: ## Profiles for v2 and diff against v1 (allocations in FS drop many times)
	$(TOOLBOX) ./scripts/profile.sh v2_flat feature-store,orchestrator v1_heavy

# --- Block 3: Fat candidates (Candidates Retrieval -> Orchestrator) and the vtprotobuf pool ---
test-v3-naive: ## Load v3_fat_naive ([]*Candidate, thousands of allocations per response)
	$(TOOLBOX) ./scripts/shoot.sh v3_naive

profile-v3-naive: ## Memory and CPU profiles for the naive contract
	$(TOOLBOX_CR) ./scripts/profile.sh v3_naive orchestrator

test-v3-vtpool: ## Load v3_fat_vtpool (vtprotobuf built-in pool out of the box)
	$(TOOLBOX) ./scripts/shoot.sh v3_vtpool

profile-v3-vtpool: ## Profiles: allocations gone, CPU goes to ResetVT/protoimpl and nested Get/Put
	$(TOOLBOX_CR) ./scripts/profile.sh v3_vtpool orchestrator v3_naive

# --- Block 4: Batch-level pooling, parallel arrays & manual reset ---
test-v4-pooled: ## Load v4_fat_pooled (one batch pool, manual field reset)
	$(TOOLBOX) ./scripts/shoot.sh v4_pooled

profile-v4-pooled: ## Profiles after a 2s warm-up: fewer UnmarshalVT allocations, less CPU on cleanup
	$(TOOLBOX_CR) ./scripts/profile.sh v4_pooled orchestrator v3_vtpool

bench-compare: ## Microbenchmarks (testing.B) summarized by benchstat
	$(TOOLBOX) sh -c "go test -run='^$$' -bench=. -benchmem -count=6 ./internal/pooling/ | tee profiles/bench.txt && echo && benchstat profiles/bench.txt"

# --- Block 5: Engineering risks (anomaly protection) ---
test-anomaly-cap: ## Unit test (no network): batch with cap > maxRetainedCap=1500 is not pooled
	$(TOOLBOX) go test -count=1 -run 'TestPutBatch' -v ./internal/pooling/

# --- Helpers ---
pprof-ui: ## pprof web UI for a saved profile (SVC, AMMO, KIND=allocs|cpu, BASE for diff)
	$(COMPOSE) exec toolbox ./scripts/pprof_ui.sh $(SVC) $(AMMO) $(KIND) $(BASE)

gen: ## Regenerate protobuf code (buf lint + buf generate) inside toolbox
	$(TOOLBOX) ./scripts/gen_proto.sh

test: ## Run all unit tests inside toolbox
	$(TOOLBOX) go test -count=1 ./...

clean-profiles: ## Remove saved profiles (they are owned by root on Linux/WSL)
	$(TOOLBOX) sh -c 'rm -f profiles/*.pb.gz profiles/*.json profiles/*.txt'
