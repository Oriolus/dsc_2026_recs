package pbpool_test

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"recsys-workshop/pkg/pb"
	"recsys-workshop/pkg/pbpool"
)

// This test guards the hand-maintained copy in api/proto/recsys/pool/v1
// against drifting from api/proto/recsys/v1/candidates_retrieval.proto.
// Linking both packages into one binary also proves there is no
// protoregistry conflict (that would panic at init).

func pairs() []struct {
	name string
	pb   proto.Message
	pool proto.Message
} {
	meta := func() *pb.Metadata {
		return &pb.Metadata{SellerId: 1<<40 + 7, CategoryId: 11, BrandId: 22, Flags: 5, ItemWeight: 1.25, WarehouseId: 33}
	}
	bids := func() *pb.AdvBids {
		return &pb.AdvBids{CampaignId: 1<<35 + 9, CpcBid: 0.75, CpmBid: 120.5, TargetSlot: 4}
	}
	cand := func(id uint64) *pb.Candidate {
		return &pb.Candidate{Id: id, Metadata: meta(), AdvBids: bids(), RetrievalScore: 0.5}
	}
	return []struct {
		name string
		pb   proto.Message
		pool proto.Message
	}{
		{"Metadata", meta(), &pbpool.Metadata{}},
		{"AdvBids", bids(), &pbpool.AdvBids{}},
		{"Candidate", cand(1), &pbpool.Candidate{}},
		{"RetrievalResponse", &pb.RetrievalResponse{Candidates: []*pb.Candidate{cand(1), cand(2), cand(3)}}, &pbpool.RetrievalResponse{}},
		{"CandidateBatchResponse", &pb.CandidateBatchResponse{
			Ids:             []uint64{1, 2, 3},
			RetrievalScores: []float32{0.1, 0.2, 0.3},
			Metadata:        []*pb.Metadata{meta(), meta(), meta()},
			AdvBids:         []*pb.AdvBids{bids(), bids(), bids()},
		}, &pbpool.CandidateBatchResponse{}},
	}
}

func TestDescriptorsInSync(t *testing.T) {
	for _, p := range pairs() {
		t.Run(p.name, func(t *testing.T) {
			compareDescriptors(t, p.pb.ProtoReflect().Descriptor(), p.pool.ProtoReflect().Descriptor())
		})
	}
}

func compareDescriptors(t *testing.T, a, b protoreflect.MessageDescriptor) {
	t.Helper()
	if a.Name() != b.Name() {
		t.Fatalf("message name: %s != %s", a.Name(), b.Name())
	}
	if a.Fields().Len() != b.Fields().Len() {
		t.Fatalf("%s: field count %d != %d", a.Name(), a.Fields().Len(), b.Fields().Len())
	}
	for i := range a.Fields().Len() {
		fa, fb := a.Fields().Get(i), b.Fields().Get(i)
		if fa.Name() != fb.Name() || fa.Number() != fb.Number() || fa.Kind() != fb.Kind() || fa.Cardinality() != fb.Cardinality() {
			t.Fatalf("%s: field mismatch: %s=%d(%s,%s) vs %s=%d(%s,%s)", a.Name(),
				fa.Name(), fa.Number(), fa.Kind(), fa.Cardinality(),
				fb.Name(), fb.Number(), fb.Kind(), fb.Cardinality())
		}
		if fa.Kind() == protoreflect.MessageKind {
			compareDescriptors(t, fa.Message(), fb.Message())
		}
	}
}

func TestWireCompatibility(t *testing.T) {
	type vtMessage interface {
		proto.Message
		MarshalVT() ([]byte, error)
		UnmarshalVT([]byte) error
	}
	det := proto.MarshalOptions{Deterministic: true}

	for _, p := range pairs() {
		t.Run(p.name+"/pb->pbpool", func(t *testing.T) {
			data, err := det.Marshal(p.pb)
			if err != nil {
				t.Fatal(err)
			}
			pool := p.pool.ProtoReflect().New().Interface().(vtMessage)
			if err := pool.UnmarshalVT(data); err != nil {
				t.Fatal(err)
			}
			if len(pool.ProtoReflect().GetUnknown()) != 0 {
				t.Fatalf("unknown fields after decoding into pbpool: field numbers out of sync")
			}
			back, err := det.Marshal(pool)
			if err != nil {
				t.Fatal(err)
			}
			if string(back) != string(data) {
				t.Fatalf("re-encoded bytes differ")
			}
		})
		t.Run(p.name+"/pbpool->pb", func(t *testing.T) {
			data, err := det.Marshal(p.pb)
			if err != nil {
				t.Fatal(err)
			}
			pool := p.pool.ProtoReflect().New().Interface().(vtMessage)
			if err := pool.UnmarshalVT(data); err != nil {
				t.Fatal(err)
			}
			poolData, err := pool.MarshalVT()
			if err != nil {
				t.Fatal(err)
			}
			got := p.pb.ProtoReflect().New().Interface()
			if err := proto.Unmarshal(poolData, got); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(got, p.pb) {
				t.Fatalf("pbpool -> pb mismatch:\n got  %v\n want %v", got, p.pb)
			}
		})
	}
}
