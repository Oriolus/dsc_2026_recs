package orchestrator

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"recsys-workshop/internal/candidates"
	"recsys-workshop/internal/features"
	"recsys-workshop/pkg/pb"
)

func newTestService(t *testing.T) (*Service, *candidates.Provider) {
	t.Helper()
	provider := candidates.NewProvider()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterCandidatesRetrievalServiceServer(srv, provider)
	pb.RegisterFeatureStoreServiceServer(srv, features.NewStore())
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(64<<20)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewService(conn, conn), provider
}

func TestAllModes(t *testing.T) {
	svc, provider := newTestService(t)
	ctx := context.Background()

	fatResults := map[pb.PipelineMode]*pb.RecommendResponse{}
	for _, mode := range []pb.PipelineMode{
		pb.PipelineMode_PIPELINE_MODE_V1_FEAST_ONEOF,
		pb.PipelineMode_PIPELINE_MODE_V2_FLAT_STRIDE,
		pb.PipelineMode_PIPELINE_MODE_V3_FAT_NAIVE,
		pb.PipelineMode_PIPELINE_MODE_V3_FAT_VTPOOL,
		pb.PipelineMode_PIPELINE_MODE_V4_FAT_POOLED,
	} {
		t.Run(mode.String(), func(t *testing.T) {
			req := &pb.RecommendRequest{UserId: 42, NumCandidates: 1000, NumFeatures: 120, Mode: mode}
			// Several calls so that pooled modes run on warmed-up, reused objects.
			var resp *pb.RecommendResponse
			for range 3 {
				var err error
				if resp, err = svc.GetRecommendations(ctx, req); err != nil {
					t.Fatal(err)
				}
			}
			if len(resp.Items) != maxResults {
				t.Fatalf("got %d items, want %d", len(resp.Items), maxResults)
			}
			for i := 1; i < len(resp.Items); i++ {
				if resp.Items[i-1].Score < resp.Items[i].Score {
					t.Fatalf("items are not sorted by score at %d", i)
				}
			}
			if mode >= pb.PipelineMode_PIPELINE_MODE_V3_FAT_NAIVE {
				fatResults[mode] = resp
			}
		})
	}

	// v3a, v3b and v4 apply the same business rules to the same candidates.
	naive := fatResults[pb.PipelineMode_PIPELINE_MODE_V3_FAT_NAIVE]
	for _, mode := range []pb.PipelineMode{pb.PipelineMode_PIPELINE_MODE_V3_FAT_VTPOOL, pb.PipelineMode_PIPELINE_MODE_V4_FAT_POOLED} {
		if !proto.Equal(naive, fatResults[mode]) {
			t.Fatalf("%s result differs from V3_FAT_NAIVE", mode)
		}
	}

	// Diversification limits hold for fat modes.
	aos, err := provider.GetFatCandidatesAoS(ctx, &pb.GetCandidatesRequest{UserId: 42, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	meta := map[uint64]*pb.Metadata{}
	for _, c := range aos.Candidates {
		meta[c.Id] = c.Metadata
	}
	brands, sellers := map[uint32]int{}, map[uint64]int{}
	for _, item := range naive.Items {
		m := meta[item.ItemId]
		brands[m.BrandId]++
		sellers[m.SellerId]++
		if brands[m.BrandId] > maxItemsPerBrand || sellers[m.SellerId] > maxItemsPerSeller {
			t.Fatalf("diversification violated for item %d", item.ItemId)
		}
	}
}

// Pooled modes must not leak data between requests of different sizes.
func TestPooledModesWithVaryingSizes(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	for _, mode := range []pb.PipelineMode{pb.PipelineMode_PIPELINE_MODE_V3_FAT_VTPOOL, pb.PipelineMode_PIPELINE_MODE_V4_FAT_POOLED} {
		for _, n := range []uint32{1000, 50, 700, 3} {
			req := &pb.RecommendRequest{UserId: uint64(n), NumCandidates: n, NumFeatures: 10, Mode: mode}
			got, err := svc.GetRecommendations(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			req.Mode = pb.PipelineMode_PIPELINE_MODE_V3_FAT_NAIVE
			want, err := svc.GetRecommendations(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(got, want) {
				t.Fatalf("%s n=%d: result differs from V3_FAT_NAIVE", mode, n)
			}
		}
	}
}

func TestInvalidArgument(t *testing.T) {
	svc, _ := newTestService(t)
	for _, req := range []*pb.RecommendRequest{
		{NumCandidates: 0, NumFeatures: 10, Mode: pb.PipelineMode_PIPELINE_MODE_V1_FEAST_ONEOF},
		{NumCandidates: 10, NumFeatures: 1, Mode: pb.PipelineMode_PIPELINE_MODE_V2_FLAT_STRIDE},
		{NumCandidates: 10, NumFeatures: 10, Mode: pb.PipelineMode_PIPELINE_MODE_UNSPECIFIED},
	} {
		_, err := svc.GetRecommendations(context.Background(), req)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("request %v: got %v, want InvalidArgument", req, err)
		}
	}
}
