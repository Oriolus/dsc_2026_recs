// Package orchestrator implements RecommendationService: it fetches
// candidates and features, scores, ranks and post-processes the result.
package orchestrator

import (
	"cmp"
	"context"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"recsys-workshop/internal/codec"
	"recsys-workshop/internal/features"
	"recsys-workshop/pkg/pb"
)

// vtCodec makes a call decode its response with the generated UnmarshalVT
// instead of the reflection-based proto.Unmarshal. Every upstream call of the
// orchestrator uses it, so iterations differ only in contracts and pooling,
// and allocation profiles point at concrete generated lines
// (e.g. `&FeatureValue{}`) instead of reflect.unsafe_New.
var vtCodec = grpc.ForceCodec(codec.VTProto{})

const (
	// maxResults is the size of the final recommendation list.
	maxResults = 100

	// Diversification limits (v3/v4 only).
	maxItemsPerBrand  = 3
	maxItemsPerSeller = 5

	// Weights of the ad bonus added to the score of fat candidates.
	cpcBidWeight = 0.5
	cpmBidWeight = 0.001

	// Fully-qualified methods of Candidates Retrieval, used with conn.Invoke
	// when the response is decoded into pkg/pbpool types.
	methodFatCandidatesAoS   = "/recsys.v1.CandidatesRetrievalService/GetFatCandidatesAoS"
	methodFatCandidatesBatch = "/recsys.v1.CandidatesRetrievalService/GetFatCandidatesBatch"
)

// Service implements pb.RecommendationServiceServer.
type Service struct {
	pb.UnimplementedRecommendationServiceServer

	retrievalConn grpc.ClientConnInterface
	retrieval     pb.CandidatesRetrievalServiceClient
	featureStore  pb.FeatureStoreServiceClient
}

// NewService creates the orchestrator on top of client connections to
// Candidates Retrieval and Feature Store.
func NewService(retrievalConn, featureStoreConn grpc.ClientConnInterface) *Service {
	return &Service{
		retrievalConn: retrievalConn,
		retrieval:     pb.NewCandidatesRetrievalServiceClient(retrievalConn),
		featureStore:  pb.NewFeatureStoreServiceClient(featureStoreConn),
	}
}

// GetRecommendations validates the request and routes it to the pipeline
// iteration selected by mode.
func (s *Service) GetRecommendations(ctx context.Context, req *pb.RecommendRequest) (*pb.RecommendResponse, error) {
	if req.GetNumCandidates() == 0 {
		return nil, status.Error(codes.InvalidArgument, "num_candidates must be > 0")
	}
	// The feature schema requires f_0 and f_1 (both vectors).
	if req.GetNumFeatures() < 2 {
		return nil, status.Error(codes.InvalidArgument, "num_features must be >= 2")
	}
	if req.GetNumFeatures() > features.MaxFeatures {
		return nil, status.Errorf(codes.InvalidArgument, "num_features must be <= %d", features.MaxFeatures)
	}

	switch req.GetMode() {
	case pb.PipelineMode_PIPELINE_MODE_V1_FEAST_ONEOF:
		return s.recommendV1Feast(ctx, req)
	case pb.PipelineMode_PIPELINE_MODE_V2_FLAT_STRIDE:
		return s.recommendV2Flat(ctx, req)
	case pb.PipelineMode_PIPELINE_MODE_V3_FAT_NAIVE:
		return s.recommendV3FatNaive(ctx, req)
	case pb.PipelineMode_PIPELINE_MODE_V3_FAT_VTPOOL:
		return s.recommendV3FatVTPool(ctx, req)
	case pb.PipelineMode_PIPELINE_MODE_V4_FAT_POOLED:
		return s.recommendV4FatBatchPool(ctx, req)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported mode %s", req.GetMode())
	}
}

// getFlatFeatures fetches the flat stride matrix (v2, v3, v4) and checks
// that it matches the requested candidates.
func (s *Service) getFlatFeatures(ctx context.Context, ids []uint64, numFeatures uint32) (*pb.FlatFeaturesResponse, error) {
	resp, err := s.featureStore.GetFeaturesV2Flat(ctx, &pb.GetFeaturesRequest{
		EntityIds:    ids,
		FeatureNames: features.FeatureNames(int(numFeatures)),
	}, vtCodec)
	if err != nil {
		return nil, err
	}
	if int(resp.GetNumEntities()) != len(ids) ||
		len(resp.GetValues()) != int(resp.GetNumEntities())*int(resp.GetStride()) {
		return nil, status.Errorf(codes.Internal,
			"feature store: inconsistent flat response (entities=%d, stride=%d, values=%d, want entities=%d)",
			resp.GetNumEntities(), resp.GetStride(), len(resp.GetValues()), len(ids))
	}
	return resp, nil
}

// flatFeatureScore sums all present features of one candidate in a flat matrix.
func flatFeatureScore(resp *pb.FlatFeaturesResponse, candidateIdx int) float32 {
	values := resp.GetValues()
	stride := int(resp.GetStride())
	var score float32
	for _, desc := range resp.GetFeatures() {
		if desc.Length == 1 {
			if v := features.GetScalarFeature(values, candidateIdx, desc, stride); !features.IsMissing(v) {
				score += v
			}
			continue
		}
		score += features.SumPresent(features.GetVectorFeature(values, candidateIdx, desc, stride))
	}
	return score
}

// adBonus is the score boost from advertising bids of a fat candidate.
func adBonus(cpcBid, cpmBid float32) float32 {
	return cpcBid*cpcBidWeight + cpmBid*cpmBidWeight
}

func byScoreDesc(a, b *pb.RecommendedItem) int {
	if c := cmp.Compare(b.Score, a.Score); c != 0 {
		return c
	}
	return cmp.Compare(a.ItemId, b.ItemId)
}

// rankTop sorts items by score and keeps the best maxResults (v1, v2).
func rankTop(items []*pb.RecommendedItem) *pb.RecommendResponse {
	slices.SortFunc(items, byScoreDesc)
	if len(items) > maxResults {
		items = items[:maxResults]
	}
	return &pb.RecommendResponse{Items: items}
}

// diversityKey carries the business attributes used for diversification.
// Values are copied out of the candidate response, so nothing references
// pooled messages after the handler returns.
type diversityKey struct {
	brandID  uint32
	sellerID uint64
}

// rankDiversified sorts items by score and greedily picks the best ones
// while limiting items per brand and per seller (v3, v4). keys[i] belongs
// to items[i]. Only the items/keys slices are reordered; candidate
// responses (including pooled batches) are never sorted in place.
func rankDiversified(items []*pb.RecommendedItem, keys []diversityKey) *pb.RecommendResponse {
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return byScoreDesc(items[a], items[b]) })

	perBrand := make(map[uint32]int)
	perSeller := make(map[uint64]int)
	result := make([]*pb.RecommendedItem, 0, min(maxResults, len(items)))
	for _, idx := range order {
		if len(result) == maxResults {
			break
		}
		key := keys[idx]
		if perBrand[key.brandID] >= maxItemsPerBrand || perSeller[key.sellerID] >= maxItemsPerSeller {
			continue
		}
		perBrand[key.brandID]++
		perSeller[key.sellerID]++
		result = append(result, items[idx])
	}
	return &pb.RecommendResponse{Items: result}
}
