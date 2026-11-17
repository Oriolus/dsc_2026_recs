package orchestrator

import (
	"context"

	"recsys-workshop/pkg/pb"
)

// recommendV3FatNaive is iteration 3a: fat AoS candidates decoded with
// UnmarshalVT into a fresh message without pooling, i.e. at least one
// Candidate, Metadata and AdvBids allocation per candidate.
func (s *Service) recommendV3FatNaive(ctx context.Context, req *pb.RecommendRequest) (*pb.RecommendResponse, error) {
	resp, err := s.fetchFatCandidatesNaive(ctx, req)
	if err != nil {
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

// fetchFatCandidatesNaive calls GetFatCandidatesAoS and decodes the response
// into a fresh pb.RetrievalResponse (no pool).
// Kept as a separate function so profiles can focus on it.
func (s *Service) fetchFatCandidatesNaive(ctx context.Context, req *pb.RecommendRequest) (*pb.RetrievalResponse, error) {
	return s.retrieval.GetFatCandidatesAoS(ctx, &pb.GetCandidatesRequest{
		UserId: req.GetUserId(),
		Limit:  req.GetNumCandidates(),
	}, vtCodec)
}
