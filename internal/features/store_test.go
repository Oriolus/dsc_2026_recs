package features

import (
	"context"
	"testing"

	"recsys-workshop/pkg/pb"
)

func TestSchema(t *testing.T) {
	const m = 120
	descs, stride := Schema(FeatureNames(m))
	if stride != m+16 || stride != Stride(m) {
		t.Fatalf("stride = %d, want %d", stride, m+16)
	}
	if len(descs) != m {
		t.Fatalf("got %d descriptors, want %d", len(descs), m)
	}
	want := []struct {
		name   string
		typ    pb.FeatureDataType
		offset uint32
		length uint32
	}{
		{"f_0", pb.FeatureDataType_FEATURE_DATA_TYPE_FLOAT_VECTOR, 0, 10},
		{"f_1", pb.FeatureDataType_FEATURE_DATA_TYPE_FLOAT_VECTOR, 10, 8},
		{"f_2", pb.FeatureDataType_FEATURE_DATA_TYPE_FLOAT, 18, 1},
		{"f_3", pb.FeatureDataType_FEATURE_DATA_TYPE_FLOAT, 19, 1},
	}
	for i, w := range want {
		d := descs[i]
		if d.Name != w.name || d.DataType != w.typ || d.Offset != w.offset || d.Length != w.length {
			t.Fatalf("descriptor %d = %v, want %+v", i, d, w)
		}
	}
	if last := descs[m-1]; last.Offset+last.Length != uint32(stride) {
		t.Fatalf("last descriptor ends at %d, want %d", last.Offset+last.Length, stride)
	}
}

// v1 and v2 must carry exactly the same values for the same request.
func TestV1AndV2Agree(t *testing.T) {
	s := NewStore()
	req := &pb.GetFeaturesRequest{EntityIds: []uint64{1, 7, 1_000_123}, FeatureNames: FeatureNames(40)}

	v1, err := s.GetFeaturesV1(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.GetFeaturesV2Flat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	stride := int(v2.Stride)
	for i, entity := range v1.Entities {
		for j, fv := range entity.Features {
			desc := v2.Features[j]
			var got []float32
			switch v := fv.Val.(type) {
			case *pb.FeatureValue_FloatVal:
				got = []float32{v.FloatVal}
			case *pb.FeatureValue_VectorVal:
				got = v.VectorVal.Values
			}
			want := GetVectorFeature(v2.Values, i, desc, stride)
			if desc.Length == 1 {
				want = []float32{GetScalarFeature(v2.Values, i, desc, stride)}
			}
			if len(got) != len(want) {
				t.Fatalf("entity %d feature %d: len %d != %d", i, j, len(got), len(want))
			}
			for k := range got {
				if got[k] != want[k] && !(IsMissing(got[k]) && IsMissing(want[k])) {
					t.Fatalf("entity %d feature %d[%d]: %v != %v", i, j, k, got[k], want[k])
				}
			}
		}
	}
}

func TestValuesContainNaN(t *testing.T) {
	var nan int
	for _, v := range randomValues[:1000] {
		if IsMissing(v) {
			nan++
		}
	}
	if nan != 50 {
		t.Fatalf("got %d NaN values per 1000, want 50 (every 20th)", nan)
	}
}
