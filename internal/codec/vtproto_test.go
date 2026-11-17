package codec_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"recsys-workshop/internal/candidates"
	"recsys-workshop/internal/codec"
	"recsys-workshop/internal/pooling"
	"recsys-workshop/pkg/pb"
	"recsys-workshop/pkg/pbpool"
)

const methodBatch = "/recsys.v1.CandidatesRetrievalService/GetFatCandidatesBatch"

func dialBufconn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterCandidatesRetrievalServiceServer(srv, candidates.NewProvider())
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// Two consecutive Invokes with codec.VTProto into the same pooled batch
// (after CustomResetBatch) must reuse batch.Metadata[i] / batch.AdvBids[i]
// pointers and produce the same result as a fresh unmarshal.
func TestVTProtoCodecReusesPooledPointers(t *testing.T) {
	conn := dialBufconn(t)
	ctx := context.Background()
	req := &pb.GetCandidatesRequest{UserId: 42, Limit: 500}

	batch := &pbpool.CandidateBatchResponse{}
	if err := conn.Invoke(ctx, methodBatch, req, batch, grpc.ForceCodec(codec.VTProto{})); err != nil {
		t.Fatal(err)
	}
	if len(batch.Metadata) != int(req.Limit) || len(batch.AdvBids) != int(req.Limit) {
		t.Fatalf("unexpected batch sizes: metadata=%d adv_bids=%d", len(batch.Metadata), len(batch.AdvBids))
	}
	metaPtrs := append([]*pbpool.Metadata(nil), batch.Metadata...)
	bidPtrs := append([]*pbpool.AdvBids(nil), batch.AdvBids...)

	pooling.CustomResetBatch(batch)
	if err := conn.Invoke(ctx, methodBatch, req, batch, grpc.ForceCodec(codec.VTProto{})); err != nil {
		t.Fatal(err)
	}
	for i := range metaPtrs {
		if batch.Metadata[i] != metaPtrs[i] {
			t.Fatalf("Metadata[%d] pointer was not reused", i)
		}
		if batch.AdvBids[i] != bidPtrs[i] {
			t.Fatalf("AdvBids[%d] pointer was not reused", i)
		}
	}

	// Fresh decode with the standard codec for comparison.
	fresh := &pbpool.CandidateBatchResponse{}
	if err := conn.Invoke(ctx, methodBatch, req, fresh); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(batch, fresh) {
		t.Fatalf("reused batch differs from a fresh unmarshal")
	}
}

// A smaller second response must not leak stale data from the first one.
func TestVTProtoCodecShrinkingBatch(t *testing.T) {
	conn := dialBufconn(t)
	ctx := context.Background()

	batch := &pbpool.CandidateBatchResponse{}
	if err := conn.Invoke(ctx, methodBatch, &pb.GetCandidatesRequest{UserId: 1, Limit: 300}, batch, grpc.ForceCodec(codec.VTProto{})); err != nil {
		t.Fatal(err)
	}
	pooling.CustomResetBatch(batch)

	req := &pb.GetCandidatesRequest{UserId: 2, Limit: 100}
	if err := conn.Invoke(ctx, methodBatch, req, batch, grpc.ForceCodec(codec.VTProto{})); err != nil {
		t.Fatal(err)
	}
	fresh := &pbpool.CandidateBatchResponse{}
	if err := conn.Invoke(ctx, methodBatch, req, fresh); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(batch, fresh) {
		t.Fatalf("reused batch differs from a fresh unmarshal")
	}
}

func TestVTProtoCodecRejectsNonVTMessages(t *testing.T) {
	if _, err := (codec.VTProto{}).Marshal(struct{}{}); err == nil {
		t.Fatal("expected error for non-vtprotobuf message")
	}
	if err := (codec.VTProto{}).Unmarshal(nil, struct{}{}); err == nil {
		t.Fatal("expected error for non-vtprotobuf message")
	}
}
