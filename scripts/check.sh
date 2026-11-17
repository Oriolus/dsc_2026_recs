#!/usr/bin/env sh
set -e
echo "1. Checking gRPC Health on all services..."
grpcurl -plaintext orchestrator:50050 grpc.health.v1.Health/Check
grpcurl -plaintext candidates-retrieval:50051 grpc.health.v1.Health/Check
grpcurl -plaintext feature-store:50052 grpc.health.v1.Health/Check
echo "2. Running end-to-end gRPC smoke call through Orchestrator..."
grpcurl -plaintext \
  -d '{"user_id": 1, "num_candidates": 10, "num_features": 10, "mode": "PIPELINE_MODE_V1_FEAST_ONEOF"}' \
  orchestrator:50050 recsys.v1.RecommendationService/GetRecommendations > /dev/null
echo "All gRPC services are healthy and communicating!"
