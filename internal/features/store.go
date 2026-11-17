// Package features implements the online Feature Store service and the
// flat-stride accessors used by the orchestrator.
package features

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"recsys-workshop/pkg/pb"
)

const (
	// MaxFeatures bounds the number of features per request.
	MaxFeatures = 1024
	// MaxEntities bounds the number of entities per request.
	MaxEntities = 10000

	// Feature schema (see FeatureLength): f_0 and f_1 are vectors, the rest are scalars.
	vector0Len = 10
	vector1Len = 8

	maxStride = MaxFeatures + vector0Len + vector1Len - 2
	rowSpace  = 1 << 20
)

// randomValues is filled once at startup so request handlers never call
// rand.Float32() N*M times. Every 20th value is NaN (a missing feature).
var randomValues []float32

// featureNames holds the canonical names f_0 ... f_{MaxFeatures-1}.
var featureNames []string

func init() {
	r := rand.New(rand.NewPCG(20261116, 42))
	randomValues = make([]float32, rowSpace+maxStride)
	for i := range randomValues {
		if i%20 == 19 {
			randomValues[i] = float32(math.NaN())
			continue
		}
		randomValues[i] = r.Float32()
	}

	featureNames = make([]string, MaxFeatures)
	for i := range featureNames {
		featureNames[i] = fmt.Sprintf("f_%d", i)
	}
}

// FeatureNames returns the canonical names of the first m features.
// The returned slice is shared and must not be modified.
func FeatureNames(m int) []string {
	return featureNames[:m:m]
}

// FeatureLength returns the length (in float32 elements) of the feature at
// position idx: f_0 is a vector of 10, f_1 a vector of 8, the rest are scalars.
func FeatureLength(idx int) int {
	switch idx {
	case 0:
		return vector0Len
	case 1:
		return vector1Len
	default:
		return 1
	}
}

// Stride returns the row length of one entity for m features (m >= 2).
func Stride(m int) int {
	return m + vector0Len + vector1Len - 2
}

// Schema builds the M feature descriptors for the given feature names.
// The layout depends only on the position of each name.
func Schema(names []string) ([]*pb.FeatureDescriptor, int) {
	descs := make([]*pb.FeatureDescriptor, len(names))
	offset := 0
	for i, name := range names {
		length := FeatureLength(i)
		dataType := pb.FeatureDataType_FEATURE_DATA_TYPE_FLOAT
		if length > 1 {
			dataType = pb.FeatureDataType_FEATURE_DATA_TYPE_FLOAT_VECTOR
		}
		descs[i] = &pb.FeatureDescriptor{
			Name:     name,
			DataType: dataType,
			Offset:   uint32(offset),
			Length:   uint32(length),
		}
		offset += length
	}
	return descs, offset
}

// rowFor returns a read-only window of pre-generated values for one entity.
func rowFor(entityID uint64, stride int) []float32 {
	start := int((entityID * 2654435761) % rowSpace)
	return randomValues[start : start+stride : start+stride]
}

// Store implements pb.FeatureStoreServiceServer.
type Store struct {
	pb.UnimplementedFeatureStoreServiceServer
}

// NewStore creates a Feature Store service.
func NewStore() *Store {
	return &Store{}
}

func validate(req *pb.GetFeaturesRequest) error {
	if n := len(req.GetEntityIds()); n == 0 || n > MaxEntities {
		return status.Errorf(codes.InvalidArgument, "entity_ids: expected 1..%d, got %d", MaxEntities, n)
	}
	if m := len(req.GetFeatureNames()); m < 2 || m > MaxFeatures {
		return status.Errorf(codes.InvalidArgument, "feature_names: expected 2..%d, got %d", MaxFeatures, m)
	}
	return nil
}

// GetFeaturesV1 returns features in Feast style: every value is wrapped
// into its own FeatureValue with a oneof, i.e. N*M objects per batch.
func (s *Store) GetFeaturesV1(_ context.Context, req *pb.GetFeaturesRequest) (*pb.GetFeaturesResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	names := req.GetFeatureNames()
	stride := Stride(len(names))

	resp := &pb.GetFeaturesResponse{
		FeatureNames: names,
		Entities:     make([]*pb.EntityFeatures, len(req.GetEntityIds())),
	}
	for i, id := range req.GetEntityIds() {
		row := rowFor(id, stride)
		values := make([]*pb.FeatureValue, len(names))
		offset := 0
		for j := range names {
			length := FeatureLength(j)
			if length == 1 {
				values[j] = &pb.FeatureValue{Val: &pb.FeatureValue_FloatVal{FloatVal: row[offset]}}
			} else {
				values[j] = &pb.FeatureValue{Val: &pb.FeatureValue_VectorVal{
					VectorVal: &pb.FloatVector{Values: row[offset : offset+length]},
				}}
			}
			offset += length
		}
		resp.Entities[i] = &pb.EntityFeatures{Features: values}
	}
	return resp, nil
}

// GetFeaturesV2Flat returns all values in one flat []float32 with stride,
// plus M descriptors describing the layout of a single row.
func (s *Store) GetFeaturesV2Flat(_ context.Context, req *pb.GetFeaturesRequest) (*pb.FlatFeaturesResponse, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	descs, stride := Schema(req.GetFeatureNames())

	ids := req.GetEntityIds()
	values := make([]float32, len(ids)*stride)
	for i, id := range ids {
		copy(values[i*stride:(i+1)*stride], rowFor(id, stride))
	}
	return &pb.FlatFeaturesResponse{
		NumEntities: uint32(len(ids)),
		Stride:      uint32(stride),
		Features:    descs,
		Values:      values,
	}, nil
}
