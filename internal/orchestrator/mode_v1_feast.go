package orchestrator

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"recsys-workshop/internal/features"
	"recsys-workshop/pkg/pb"
)

// recommendV1Feast is iteration 1: thin candidates + Feast-like features,
// where every value is a separate FeatureValue with a oneof (N*M objects).
func (s *Service) recommendV1Feast(ctx context.Context, req *pb.RecommendRequest) (*pb.RecommendResponse, error) {
	candidates, err := s.retrieval.GetThinCandidates(ctx, &pb.GetCandidatesRequest{
		UserId: req.GetUserId(),
		Limit:  req.GetNumCandidates(),
	}, vtCodec)
	if err != nil {
		return nil, err
	}
	ids := candidates.GetIds()

	feats, err := s.featureStore.GetFeaturesV1(ctx, &pb.GetFeaturesRequest{
		EntityIds:    ids,
		FeatureNames: features.FeatureNames(int(req.GetNumFeatures())),
	}, vtCodec)
	if err != nil {
		return nil, err
	}
	if len(feats.GetEntities()) != len(ids) {
		return nil, status.Errorf(codes.Internal, "feature store: got %d entities, want %d", len(feats.GetEntities()), len(ids))
	}

	items := make([]*pb.RecommendedItem, 0, len(ids))
	for i, entity := range feats.GetEntities() {
		var score float32
		for _, fv := range entity.GetFeatures() {
			switch v := fv.GetVal().(type) {
			case *pb.FeatureValue_FloatVal:
				if !features.IsMissing(v.FloatVal) {
					score += v.FloatVal
				}
			case *pb.FeatureValue_VectorVal:
				score += features.SumPresent(v.VectorVal.GetValues())
			}
		}
		items = append(items, &pb.RecommendedItem{ItemId: ids[i], Score: score})
	}
	return rankTop(items), nil
}
