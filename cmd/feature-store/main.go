// Command feature-store serves online features for candidates.
package main

import (
	"google.golang.org/grpc"

	"recsys-workshop/internal/features"
	"recsys-workshop/internal/server"
	"recsys-workshop/pkg/pb"
)

func main() {
	server.Run("feature-store",
		server.Env("GRPC_ADDR", "0.0.0.0:50052"),
		server.Env("PPROF_ADDR", "0.0.0.0:6062"),
		func(s *grpc.Server) {
			pb.RegisterFeatureStoreServiceServer(s, features.NewStore())
		})
}
