// Package pooling implements batch-level pooling of Candidates Retrieval
// responses with manual field reset (iteration 4).
package pooling

import (
	"sync"

	"recsys-workshop/pkg/pbpool"
)

// maxRetainedCap protects the pool from retaining huge buffers after
// anomalous requests.
const maxRetainedCap = 1500

var batchPool = sync.Pool{
	New: func() any {
		return new(pbpool.CandidateBatchResponse)
	},
}

// GetBatch takes a batch from the pool.
func GetBatch() *pbpool.CandidateBatchResponse {
	return batchPool.Get().(*pbpool.CandidateBatchResponse)
}

// PutBatch resets the batch and returns it to the pool unless it is oversized.
func PutBatch(b *pbpool.CandidateBatchResponse) {
	if b == nil || cap(b.Ids) > maxRetainedCap {
		return // Protection against retaining huge buffers after anomalous requests
	}
	CustomResetBatch(b)
	batchPool.Put(b)
}

// CustomResetBatch clears the batch while keeping every pointer in the
// backing arrays of Metadata and AdvBids, so the next UnmarshalVT reuses them.
func CustomResetBatch(x *pbpool.CandidateBatchResponse) {
	x.Ids = x.Ids[:0]
	x.RetrievalScores = x.RetrievalScores[:0]

	// Reset structs in place, do NOT overwrite pointers in the backing array!
	for _, m := range x.Metadata[:cap(x.Metadata)] {
		if m != nil {
			resetMetadata(m)
		}
	}
	x.Metadata = x.Metadata[:0]

	for _, a := range x.AdvBids[:cap(x.AdvBids)] {
		if a != nil {
			resetAdvBids(a)
		}
	}
	x.AdvBids = x.AdvBids[:0]
}

// resetMetadata zeroes public fields only. Do not use *m = pbpool.Metadata{}:
// it wipes protoimpl.MessageState and forces the protobuf runtime to
// re-initialize it, which is exactly the work done by the generated Reset().
func resetMetadata(m *pbpool.Metadata) {
	m.SellerId = 0
	m.CategoryId = 0
	m.BrandId = 0
	m.Flags = 0
	m.ItemWeight = 0
	m.WarehouseId = 0
}

// resetAdvBids zeroes public fields only (see resetMetadata).
func resetAdvBids(a *pbpool.AdvBids) {
	a.CampaignId = 0
	a.CpcBid = 0
	a.CpmBid = 0
	a.TargetSlot = 0
}
