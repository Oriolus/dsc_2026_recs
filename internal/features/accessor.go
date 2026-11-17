package features

import (
	"math"

	"recsys-workshop/pkg/pb"
)

// GetScalarFeature reads a scalar feature (length == 1) of one candidate.
func GetScalarFeature(values []float32, candidateIdx int, desc *pb.FeatureDescriptor, stride int) float32 {
	idx := candidateIdx*stride + int(desc.Offset)
	return values[idx]
}

// GetVectorFeature reads a vector feature (length > 1) of one candidate.
// The returned slice aliases values and must not be modified.
func GetVectorFeature(values []float32, candidateIdx int, desc *pb.FeatureDescriptor, stride int) []float32 {
	start := candidateIdx*stride + int(desc.Offset)
	return values[start : start+int(desc.Length)]
}

// IsMissing reports whether a feature value is absent (encoded as NaN).
func IsMissing(v float32) bool {
	return math.IsNaN(float64(v))
}

// SumPresent sums all values that are not NaN.
func SumPresent(values []float32) float32 {
	var sum float32
	for _, v := range values {
		if !IsMissing(v) {
			sum += v
		}
	}
	return sum
}
