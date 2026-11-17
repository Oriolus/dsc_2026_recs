// Package codec provides a gRPC codec that calls vtprotobuf methods directly.
package codec

import "fmt"

type vtMessage interface {
	MarshalVT() ([]byte, error)
	UnmarshalVT([]byte) error
}

// VTProto is a gRPC codec that calls vtprotobuf methods directly.
// Unlike the default codec it does not reset the target message before
// unmarshaling, so pointers pre-allocated in pooled messages are reused.
//
// It is intentionally NOT registered globally: the orchestrator passes it per
// call with grpc.ForceCodec(codec.VTProto{}), servers keep the standard codec.
// When the target message is reused, the caller is responsible for clearing
// it first (ResetVT in v3b, CustomResetBatch in v4).
type VTProto struct{}

func (VTProto) Marshal(v any) ([]byte, error) {
	m, ok := v.(vtMessage)
	if !ok {
		return nil, fmt.Errorf("codec: %T does not implement vtprotobuf methods", v)
	}
	return m.MarshalVT()
}

func (VTProto) Unmarshal(data []byte, v any) error {
	m, ok := v.(vtMessage)
	if !ok {
		return fmt.Errorf("codec: %T does not implement vtprotobuf methods", v)
	}
	return m.UnmarshalVT(data)
}

// Name must be "proto": it becomes the content-subtype on the wire,
// and the server only has the standard proto codec registered.
func (VTProto) Name() string { return "proto" }
