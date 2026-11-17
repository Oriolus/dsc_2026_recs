// Command orchestrator serves personal recommendations (RecommendationService).
package main

import (
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"recsys-workshop/internal/orchestrator"
	"recsys-workshop/internal/server"
	"recsys-workshop/pkg/pb"
)

// maxRecvMsgSize allows large feature responses (v1 with big N*M).
const maxRecvMsgSize = 64 << 20

func dial(addr string) *grpc.ClientConn {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxRecvMsgSize)),
		grpc.WithStatsHandler(server.PayloadStats{}),
	)
	if err != nil {
		log.Fatalf("orchestrator: dial %s: %v", addr, err)
	}
	return conn
}

func main() {
	retrievalConn := dial(server.Env("CANDIDATES_ADDR", "localhost:50051"))
	defer retrievalConn.Close()
	featureStoreConn := dial(server.Env("FEATURE_STORE_ADDR", "localhost:50052"))
	defer featureStoreConn.Close()

	server.Run("orchestrator",
		server.Env("GRPC_ADDR", "0.0.0.0:50050"),
		server.Env("PPROF_ADDR", "0.0.0.0:6060"),
		func(s *grpc.Server) {
			pb.RegisterRecommendationServiceServer(s, orchestrator.NewService(retrievalConn, featureStoreConn))
		})
}
