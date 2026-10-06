// Command payment-api запускает HTTP API платёжного сервиса.
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/acquirer"
	"github.com/cagitic1-source/payflow/features/payment/memory"
	"github.com/cagitic1-source/payflow/features/payment/service"
	"github.com/cagitic1-source/payflow/features/payment/transport/httptransport"
	"github.com/cagitic1-source/payflow/internal/app/http/v1/router"
	"github.com/cagitic1-source/payflow/internal/app/http/v1/server"
	"github.com/cagitic1-source/payflow/internal/workerpool"
)

const (
	workers         = 20
	queueCapacity   = 100
	acquirerTimeout = 5 * time.Second
	// Бюджет остановки: HTTP (10 с) + пул (15 с) < 30 с, которые Kubernetes
	// даёт поду между SIGTERM и SIGKILL.
	httpShutdownTimeout = 10 * time.Second
	poolStopTimeout     = 15 * time.Second
	// idempotencyKeyTTL - сколько живёт ключ идемпотентности: в течение этого
	// времени повтор запроса вернёт уже созданный платёж.
	idempotencyKeyTTL = 24 * time.Hour
)

// idempotencyCleanupInterval - как часто удалять истёкшие ключи идемпотентности.
const idempotencyCleanupInterval = time.Hour

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	// SIGINT (Ctrl+C) и SIGTERM (docker stop, Kubernetes) отменяют ctx.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Порт открываем первым: если он занят, выходим до запуска горутин.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	repo := memory.NewPaymentRepository()
	idempotency := memory.NewIdempotencyStore(idempotencyKeyTTL)

	bank := acquirer.NewFake(acquirer.DefaultFakeConfig())
	processor := service.NewProcessor(repo, bank, acquirerTimeout, logger)
	pool := workerpool.New(workers, queueCapacity, processor.Process, logger)

	go idempotency.RunCleanup(ctx, idempotencyCleanupInterval)

	svc := service.NewPaymentService(repo, idempotency, pool)
	handler := router.New(logger, httptransport.NewPaymentHandler(svc, logger))

	serverErr := server.Run(ctx, ln, handler, cfg.server, logger)

	// Порядок остановки: HTTP-сервер уже не принимает запросы - значит,
	// новых платежей не будет. Теперь дорабатываем очередь.
	stopCtx, cancel := context.WithTimeout(context.Background(), poolStopTimeout)
	defer cancel()
	if err := pool.Stop(stopCtx); err != nil {
		logger.Error("worker pool stopped before queue was drained", zap.Error(err))
	}

	return serverErr
}
