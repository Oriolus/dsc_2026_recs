// Command candidates-retrieval serves thin and fat candidates.
package main

import (
	"google.golang.org/grpc"

	"recsys-workshop/internal/candidates"
	"recsys-workshop/internal/server"
	"recsys-workshop/pkg/pb"
)

func main() {
	server.Run("candidates-retrieval",
		server.Env("GRPC_ADDR", "0.0.0.0:50051"),
		server.Env("PPROF_ADDR", "0.0.0.0:6061"),
		func(s *grpc.Server) {
			pb.RegisterCandidatesRetrievalServiceServer(s, candidates.NewProvider())
		})
}
