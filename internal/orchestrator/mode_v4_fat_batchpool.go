package orchestrator

import (
	"context"

	"recsys-workshop/internal/pooling"
	"recsys-workshop/pkg/pb"
	"recsys-workshop/pkg/pbpool"
)

// recommendV4FatBatchPool is iteration 4: parallel arrays (SoA) decoded into
// a single pooled batch with manual field reset. UnmarshalVT reuses the
// pointers already warmed up inside cap(batch.Metadata) / cap(batch.AdvBids).
//
// Never sort or overwrite pointers inside batch: that would break the
// unique-pointer invariant of the pooled backing arrays. Only the resulting
// items slice is sorted and filtered.
func (s *Service) recommendV4FatBatchPool(ctx context.Context, req *pb.RecommendRequest) (*pb.RecommendResponse, error) {
	batch := pooling.GetBatch()
	defer pooling.PutBatch(batch)

	if err := s.fetchFatCandidatesBatch(ctx, req, batch); err != nil {
		return nil, err
	}

	minLen := min(len(batch.Ids), len(batch.RetrievalScores), len(batch.Metadata), len(batch.AdvBids))
	ids := batch.Ids[:minLen]

	feats, err := s.getFlatFeatures(ctx, ids, req.GetNumFeatures())
	if err != nil {
		return nil, err
	}

	items := make([]*pb.RecommendedItem, 0, minLen)
	keys := make([]diversityKey, 0, minLen)
	for i := range minLen {
		meta, bids := batch.Metadata[i], batch.AdvBids[i]
		score := flatFeatureScore(feats, i) + batch.RetrievalScores[i] + adBonus(bids.GetCpcBid(), bids.GetCpmBid())
		items = append(items, &pb.RecommendedItem{ItemId: ids[i], Score: score})
		keys = append(keys, diversityKey{brandID: meta.GetBrandId(), sellerID: meta.GetSellerId()})
	}
	return rankDiversified(items, keys), nil
}

// fetchFatCandidatesBatch calls GetFatCandidatesBatch and decodes the response
// into the pooled batch with UnmarshalVT, reusing pointers inside cap.
// Kept as a separate function so profiles can focus on it.
func (s *Service) fetchFatCandidatesBatch(ctx context.Context, req *pb.RecommendRequest, batch *pbpool.CandidateBatchResponse) error {
	return s.retrievalConn.Invoke(ctx, methodFatCandidatesBatch, &pb.GetCandidatesRequest{
		UserId: req.GetUserId(),
		Limit:  req.GetNumCandidates(),
	}, batch, vtCodec)
}
