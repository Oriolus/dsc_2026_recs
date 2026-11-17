package orchestrator

import (
	"context"

	"recsys-workshop/pkg/pb"
	"recsys-workshop/pkg/pbpool"
)

// recommendV3FatVTPool is iteration 3b: fat AoS candidates decoded into the
// vtprotobuf built-in pool "out of the box". Unmarshal allocations are gone,
// but the generated ResetVT()/ReturnToVTPool() now cost CPU: Reset() with
// protoimpl bookkeeping for every object and per-candidate Get/Put of the
// nested Metadata and AdvBids.
func (s *Service) recommendV3FatVTPool(ctx context.Context, req *pb.RecommendRequest) (*pb.RecommendResponse, error) {
	resp := pbpool.RetrievalResponseFromVTPool()
	defer resp.ReturnToVTPool()

	if err := s.fetchFatCandidatesVTPool(ctx, req, resp); err != nil {
		return nil, err
	}
	candidates := resp.GetCandidates()

	ids := make([]uint64, len(candidates))
	for i, c := range candidates {
		ids[i] = c.GetId()
	}
	feats, err := s.getFlatFeatures(ctx, ids, req.GetNumFeatures())
	if err != nil {
		return nil, err
	}

	items := make([]*pb.RecommendedItem, 0, len(candidates))
	keys := make([]diversityKey, 0, len(candidates))
	for i, c := range candidates {
		score := flatFeatureScore(feats, i) + c.GetRetrievalScore() +
			adBonus(c.GetAdvBids().GetCpcBid(), c.GetAdvBids().GetCpmBid())
		items = append(items, &pb.RecommendedItem{ItemId: c.GetId(), Score: score})
		keys = append(keys, diversityKey{brandID: c.GetMetadata().GetBrandId(), sellerID: c.GetMetadata().GetSellerId()})
	}
	return rankDiversified(items, keys), nil
}

// fetchFatCandidatesVTPool calls GetFatCandidatesAoS and decodes the response
// into a pooled message with UnmarshalVT (no reset before unmarshaling).
// Kept as a separate function so profiles can focus on it.
func (s *Service) fetchFatCandidatesVTPool(ctx context.Context, req *pb.RecommendRequest, resp *pbpool.RetrievalResponse) error {
	return s.retrievalConn.Invoke(ctx, methodFatCandidatesAoS, &pb.GetCandidatesRequest{
		UserId: req.GetUserId(),
		Limit:  req.GetNumCandidates(),
	}, resp, vtCodec)
}
