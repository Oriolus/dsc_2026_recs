package orchestrator

import (
	"context"

	"recsys-workshop/pkg/pb"
)

// recommendV2Flat is iteration 2: thin candidates + 1D flat stride matrix
// with M feature descriptors per batch.
func (s *Service) recommendV2Flat(ctx context.Context, req *pb.RecommendRequest) (*pb.RecommendResponse, error) {
	candidates, err := s.retrieval.GetThinCandidates(ctx, &pb.GetCandidatesRequest{
		UserId: req.GetUserId(),
		Limit:  req.GetNumCandidates(),
	}, vtCodec)
	if err != nil {
		return nil, err
	}
	ids := candidates.GetIds()

	feats, err := s.getFlatFeatures(ctx, ids, req.GetNumFeatures())
	if err != nil {
		return nil, err
	}

	items := make([]*pb.RecommendedItem, 0, len(ids))
	for i, id := range ids {
		items = append(items, &pb.RecommendedItem{ItemId: id, Score: flatFeatureScore(feats, i)})
	}
	return rankTop(items), nil
}
