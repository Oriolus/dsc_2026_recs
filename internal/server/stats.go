package server

import (
	"context"
	"expvar"

	"google.golang.org/grpc/stats"
)

// Response wire sizes per full gRPC method, exported at /debug/vars on the
// pprof port. scripts/shoot.sh reads them to report average response sizes.
var (
	responseBytes = expvar.NewMap("grpc_response_bytes")
	responseCount = expvar.NewMap("grpc_response_count")
)

type methodKey struct{}

// PayloadStats is a gRPC stats.Handler that records response wire sizes:
// responses sent by a server and responses received by a client.
type PayloadStats struct{}

var _ stats.Handler = PayloadStats{}

func (PayloadStats) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	return context.WithValue(ctx, methodKey{}, info.FullMethodName)
}

func (PayloadStats) HandleRPC(ctx context.Context, s stats.RPCStats) {
	var size int
	switch p := s.(type) {
	case *stats.OutPayload:
		if !p.IsClient() {
			size = p.WireLength
		}
	case *stats.InPayload:
		if p.IsClient() {
			size = p.WireLength
		}
	}
	if size == 0 {
		return
	}
	method, _ := ctx.Value(methodKey{}).(string)
	responseBytes.Add(method, int64(size))
	responseCount.Add(method, 1)
}

func (PayloadStats) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context { return ctx }

func (PayloadStats) HandleConn(context.Context, stats.ConnStats) {}
