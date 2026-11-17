// Package candidates implements the Candidates Retrieval service.
package candidates

import (
	"context"
	"math/rand/v2"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"recsys-workshop/pkg/pb"
)

const (
	// MaxLimit bounds the number of candidates per request.
	MaxLimit = 10000

	catalogSize = 1 << 16
	numBrands   = 40
	numSellers  = 150
)

// catalog is generated once at startup. Its parallel arrays are longer than
// catalogSize by MaxLimit, so any request is served by sub-slicing a
// contiguous window without copying or allocating per candidate.
type catalog struct {
	ids        []uint64
	scores     []float32
	metadata   []*pb.Metadata
	advBids    []*pb.AdvBids
	candidates []*pb.Candidate
}

func newCatalog() *catalog {
	r := rand.New(rand.NewPCG(7, 2026))
	n := catalogSize + MaxLimit
	c := &catalog{
		ids:        make([]uint64, n),
		scores:     make([]float32, n),
		metadata:   make([]*pb.Metadata, n),
		advBids:    make([]*pb.AdvBids, n),
		candidates: make([]*pb.Candidate, n),
	}
	for i := range n {
		c.ids[i] = 1_000_000 + uint64(i)
		c.scores[i] = r.Float32()
		c.metadata[i] = &pb.Metadata{
			SellerId:    10_000 + uint64(r.IntN(numSellers)),
			CategoryId:  uint32(r.IntN(500)),
			BrandId:     uint32(1 + r.IntN(numBrands)),
			Flags:       uint32(r.IntN(16)),
			ItemWeight:  0.1 + r.Float32()*5,
			WarehouseId: uint32(1 + r.IntN(30)),
		}
		c.advBids[i] = &pb.AdvBids{
			CampaignId: 500_000 + uint64(r.IntN(5000)),
			CpcBid:     r.Float32() * 2,
			CpmBid:     r.Float32() * 300,
			TargetSlot: uint32(r.IntN(10)),
		}
		c.candidates[i] = &pb.Candidate{
			Id:             c.ids[i],
			Metadata:       c.metadata[i],
			AdvBids:        c.advBids[i],
			RetrievalScore: c.scores[i],
		}
	}
	return c
}

// Provider implements pb.CandidatesRetrievalServiceServer.
type Provider struct {
	pb.UnimplementedCandidatesRetrievalServiceServer
	catalog *catalog
}

// NewProvider creates a Candidates Retrieval service with a deterministic catalog.
func NewProvider() *Provider {
	return &Provider{catalog: newCatalog()}
}

// window returns the [start, end) catalog range for a request.
func window(req *pb.GetCandidatesRequest) (int, int, error) {
	limit := int(req.GetLimit())
	if limit == 0 || limit > MaxLimit {
		return 0, 0, status.Errorf(codes.InvalidArgument, "limit: expected 1..%d, got %d", MaxLimit, limit)
	}
	start := int((req.GetUserId() * 0x9E3779B97F4A7C15) % catalogSize)
	return start, start + limit, nil
}

// GetThinCandidates returns candidate IDs only.
func (p *Provider) GetThinCandidates(_ context.Context, req *pb.GetCandidatesRequest) (*pb.ThinCandidatesResponse, error) {
	start, end, err := window(req)
	if err != nil {
		return nil, err
	}
	return &pb.ThinCandidatesResponse{Ids: p.catalog.ids[start:end]}, nil
}

// GetFatCandidatesAoS returns candidates as an array of structs with nested
// Metadata and AdvBids.
func (p *Provider) GetFatCandidatesAoS(_ context.Context, req *pb.GetCandidatesRequest) (*pb.RetrievalResponse, error) {
	start, end, err := window(req)
	if err != nil {
		return nil, err
	}
	return &pb.RetrievalResponse{Candidates: p.catalog.candidates[start:end]}, nil
}

// GetFatCandidatesBatch returns the same candidates as parallel arrays.
// All four slices always have the same length.
func (p *Provider) GetFatCandidatesBatch(_ context.Context, req *pb.GetCandidatesRequest) (*pb.CandidateBatchResponse, error) {
	start, end, err := window(req)
	if err != nil {
		return nil, err
	}
	return &pb.CandidateBatchResponse{
		Ids:             p.catalog.ids[start:end],
		RetrievalScores: p.catalog.scores[start:end],
		Metadata:        p.catalog.metadata[start:end],
		AdvBids:         p.catalog.advBids[start:end],
	}, nil
}
