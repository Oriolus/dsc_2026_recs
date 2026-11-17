package pooling

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	"recsys-workshop/internal/candidates"
	"recsys-workshop/pkg/pb"
	"recsys-workshop/pkg/pbpool"
)

const benchCandidates = 1000

// Payloads are produced by the real Candidates Retrieval implementation.
func payloads(b *testing.B) (aos, batch []byte) {
	b.Helper()
	ctx := context.Background()
	p := candidates.NewProvider()
	req := &pb.GetCandidatesRequest{UserId: 42, Limit: benchCandidates}

	aosResp, err := p.GetFatCandidatesAoS(ctx, req)
	if err != nil {
		b.Fatal(err)
	}
	batchResp, err := p.GetFatCandidatesBatch(ctx, req)
	if err != nil {
		b.Fatal(err)
	}
	if aos, err = proto.Marshal(aosResp); err != nil {
		b.Fatal(err)
	}
	if batch, err = proto.Marshal(batchResp); err != nil {
		b.Fatal(err)
	}
	return aos, batch
}

var sink int

// Reference: reflection-based proto.Unmarshal (the default gRPC codec).
// Same allocations as AoS_Naive, slower decoding: the codec is not the problem.
func BenchmarkUnmarshal_AoS_ProtoReflect(b *testing.B) {
	data, _ := payloads(b)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		resp := &pb.RetrievalResponse{}
		if err := proto.Unmarshal(data, resp); err != nil {
			b.Fatal(err)
		}
		sink += len(resp.Candidates)
	}
}

// Iteration 3a: generated UnmarshalVT into a fresh []*Candidate, no pool.
func BenchmarkUnmarshal_AoS_Naive(b *testing.B) {
	data, _ := payloads(b)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		resp := &pb.RetrievalResponse{}
		if err := resp.UnmarshalVT(data); err != nil {
			b.Fatal(err)
		}
		sink += len(resp.Candidates)
	}
}

// Iteration 3b: vtprotobuf built-in pool, generated ResetVT/ReturnToVTPool.
func BenchmarkUnmarshal_AoS_VTPool(b *testing.B) {
	data, _ := payloads(b)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		resp := pbpool.RetrievalResponseFromVTPool()
		if err := resp.UnmarshalVT(data); err != nil {
			b.Fatal(err)
		}
		sink += len(resp.Candidates)
		resp.ReturnToVTPool()
	}
}

// Iteration 4: parallel arrays, one batch pool, manual field reset.
func BenchmarkUnmarshal_BatchPool_CustomReset(b *testing.B) {
	_, data := payloads(b)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		batch := GetBatch()
		if err := batch.UnmarshalVT(data); err != nil {
			b.Fatal(err)
		}
		sink += len(batch.Ids)
		PutBatch(batch)
	}
}
