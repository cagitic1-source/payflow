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

	"github.com/cagitic1-source/payflow/features/payment/memory"
	"github.com/cagitic1-source/payflow/features/payment/service"
	"github.com/cagitic1-source/payflow/features/payment/transport/httptransport"
	"github.com/cagitic1-source/payflow/internal/app/http/v1/router"
	"github.com/cagitic1-source/payflow/internal/app/http/v1/server"
)

// idempotencyKeyTTL - сколько живёт ключ идемпотентности: в течение этого
// времени повтор запроса вернёт уже созданный платёж.
const idempotencyKeyTTL = 24 * time.Hour

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	// SIGINT (Ctrl+C) и SIGTERM (docker stop, Kubernetes) отменяют ctx.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo := memory.NewPaymentRepository()
	idempotency := memory.NewIdempotencyStore(idempotencyKeyTTL)
	svc := service.NewPaymentService(repo, idempotency)
	handler := router.New(logger, httptransport.NewPaymentHandler(svc, logger))

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", ":8080")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	return server.Run(ctx, ln, handler, server.DefaultConfig(), logger)
}
