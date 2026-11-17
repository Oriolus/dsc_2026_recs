// Package server contains the shared bootstrap of the workshop gRPC services:
// health checks, server reflection, net/http/pprof and graceful shutdown.
package server

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof" // Registers /debug/pprof handlers on http.DefaultServeMux.
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Env returns the value of an environment variable or a default.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Run serves gRPC on grpcAddr and pprof on pprofAddr until SIGINT/SIGTERM.
// register attaches the service implementations to the gRPC server.
func Run(name, grpcAddr, pprofAddr string, register func(*grpc.Server)) {
	// pprof listens on 0.0.0.0 so the toolbox container can reach it.
	pprofSrv := &http.Server{Addr: pprofAddr, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("%s: pprof listening on %s", name, pprofAddr)
		if err := pprofSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("%s: pprof: %v", name, err)
		}
	}()

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("%s: listen %s: %v", name, grpcAddr, err)
	}

	grpcServer := grpc.NewServer(grpc.StatsHandler(PayloadStats{}))
	register(grpcServer)

	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthSrv)
	reflection.Register(grpcServer)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Printf("%s: shutting down", name)
		healthSrv.Shutdown()
		grpcServer.GracefulStop()
		_ = pprofSrv.Close()
	}()

	log.Printf("%s: gRPC listening on %s", name, grpcAddr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("%s: serve: %v", name, err)
	}
}
