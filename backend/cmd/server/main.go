package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"trace-topo/internal/anomaly"
	grpcserver "trace-topo/internal/grpc"
	"trace-topo/internal/api"
	"trace-topo/internal/config"
	"trace-topo/internal/sampling"
	"trace-topo/internal/storage"
	"trace-topo/internal/topology"
	"trace-topo/internal/trace"
)

func main() {
	config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logrus.Infof("Received signal: %v, shutting down...", sig)
		cancel()
	}()

	store, err := storage.NewPostgresStore(ctx)
	if err != nil {
		logrus.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()

	anomalyDetector := anomaly.NewDetector(store)
	topologyDiscoverer := topology.NewDiscoverer(store)
	samplingEngine := sampling.NewEngine(store, anomalyDetector)

	assembler := trace.NewAssembler(store, topologyDiscoverer, anomalyDetector, samplingEngine)
	assembler.Start(ctx)

	topologyDiscoverer.Start(ctx)

	grpcServer := grpcserver.NewTraceReceiverServer(assembler)
	if err := grpcServer.Start(ctx); err != nil {
		logrus.Fatalf("Failed to start gRPC server: %v", err)
	}

	handler := api.NewHandler(store, topologyDiscoverer, samplingEngine, assembler)
	if err := handler.Start(ctx); err != nil {
		logrus.Fatalf("Failed to start API server: %v", err)
	}

	logrus.Info("Trace Topology server started successfully")

	<-ctx.Done()
	logrus.Info("Shutdown complete")
}
