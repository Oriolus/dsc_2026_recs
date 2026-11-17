package pooling

import (
	"testing"

	"recsys-workshop/pkg/pbpool"
)

func filledBatch(n, capacity int) *pbpool.CandidateBatchResponse {
	b := &pbpool.CandidateBatchResponse{
		Ids:             make([]uint64, n, capacity),
		RetrievalScores: make([]float32, n, capacity),
		Metadata:        make([]*pbpool.Metadata, n, capacity),
		AdvBids:         make([]*pbpool.AdvBids, n, capacity),
	}
	for i := range n {
		b.Ids[i] = uint64(i + 1)
		b.RetrievalScores[i] = 0.5
		b.Metadata[i] = &pbpool.Metadata{SellerId: 1, CategoryId: 2, BrandId: 3, Flags: 4, ItemWeight: 5, WarehouseId: 6}
		b.AdvBids[i] = &pbpool.AdvBids{CampaignId: 1, CpcBid: 2, CpmBid: 3, TargetSlot: 4}
	}
	return b
}

func TestCustomResetBatchKeepsPointers(t *testing.T) {
	b := filledBatch(10, 16)
	meta := append([]*pbpool.Metadata(nil), b.Metadata...)
	bids := append([]*pbpool.AdvBids(nil), b.AdvBids...)

	CustomResetBatch(b)

	if len(b.Ids) != 0 || len(b.RetrievalScores) != 0 || len(b.Metadata) != 0 || len(b.AdvBids) != 0 {
		t.Fatal("lengths were not reset to zero")
	}
	for i := range meta {
		if b.Metadata[:cap(b.Metadata)][i] != meta[i] || b.AdvBids[:cap(b.AdvBids)][i] != bids[i] {
			t.Fatalf("pointer %d in backing array was overwritten", i)
		}
		if meta[i].SellerId != 0 || meta[i].BrandId != 0 || meta[i].ItemWeight != 0 {
			t.Fatalf("Metadata[%d] fields were not reset: %v", i, meta[i])
		}
		if bids[i].CampaignId != 0 || bids[i].CpcBid != 0 || bids[i].CpmBid != 0 || bids[i].TargetSlot != 0 {
			t.Fatalf("AdvBids[%d] fields were not reset: %v", i, bids[i])
		}
	}
}

// Anomaly protection: a batch whose cap exceeds maxRetainedCap is dropped
// instead of being reset and returned to the pool.
func TestPutBatchDropsOversizedBatch(t *testing.T) {
	b := filledBatch(maxRetainedCap+500, maxRetainedCap+500)

	PutBatch(b)

	if len(b.Ids) != maxRetainedCap+500 || b.Metadata[0].SellerId == 0 {
		t.Fatal("oversized batch was reset, i.e. it was about to be returned to the pool")
	}
	for range 100 {
		if GetBatch() == b {
			t.Fatal("oversized batch was returned to the pool")
		}
	}
}

func TestPutBatchAcceptsBatchAtLimit(t *testing.T) {
	b := filledBatch(maxRetainedCap, maxRetainedCap)

	PutBatch(b)

	if len(b.Ids) != 0 || b.Metadata[:1][0].SellerId != 0 {
		t.Fatal("batch at the limit was not reset before returning to the pool")
	}
}

func TestPutBatchNil(t *testing.T) {
	PutBatch(nil) // must not panic
}
