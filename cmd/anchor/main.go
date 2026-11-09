// Command anchor batches verified log hashes into Merkle roots, records them on Tapyrus
// and stores block data once the transaction is in a block.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"smapass-back/internal/di"
	"smapass-back/internal/infrastructure/config"
	"smapass-back/internal/usecase"
)

func main() {
	if err := run(); err != nil {
		slog.Error("anchor worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadAnchor()
	if err != nil {
		return err
	}
	startup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	worker, cleanup, err := di.InitializeAnchorWorker(startup, cfg)
	if err != nil {
		return err
	}
	defer cleanup()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	slog.Info("anchor worker started", "chain", cfg.Chain, "batch_interval", cfg.BatchInterval.String(), "poll_interval", cfg.PollInterval.String())

	batch := time.NewTicker(cfg.BatchInterval)
	defer batch.Stop()
	poll := time.NewTicker(cfg.PollInterval)
	defer poll.Stop()
	createBatch(ctx, worker)
	process(ctx, worker)
	for {
		select {
		case <-ctx.Done():
			slog.Info("anchor worker stopping")
			return nil
		case <-batch.C:
			createBatch(ctx, worker)
		case <-poll.C:
			process(ctx, worker)
		}
	}
}

// Errors are logged and retried on the next tick; log IDs, payloads and IMEIs are never logged.
func createBatch(ctx context.Context, worker *usecase.AnchorUsecase) {
	anchor, err := worker.Batch(ctx)
	switch {
	case err != nil && ctx.Err() == nil:
		slog.Error("anchor batch failed", "error", err)
	case anchor != nil:
		slog.Info("anchor batch created", "anchor_id", anchor.ID, "leaves", anchor.LeafCount)
	}
}

func process(ctx context.Context, worker *usecase.AnchorUsecase) {
	n, err := worker.Process(ctx)
	switch {
	case err != nil && ctx.Err() == nil:
		slog.Error("anchor processing failed", "error", err)
	case n > 0:
		slog.Info("anchors processed", "count", n)
	}
}
