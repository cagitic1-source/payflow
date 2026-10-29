// Command payment-api запускает HTTP API платёжного сервиса.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/acquirer"
	"github.com/cagitic1-source/payflow/features/payment/memory"
	"github.com/cagitic1-source/payflow/features/payment/service"
	"github.com/cagitic1-source/payflow/features/payment/transport/httptransport"
	"github.com/cagitic1-source/payflow/internal/app/http/middleware"
	"github.com/cagitic1-source/payflow/internal/app/http/v1/router"
	"github.com/cagitic1-source/payflow/internal/app/http/v1/server"
	"github.com/cagitic1-source/payflow/internal/metrics"
	"github.com/cagitic1-source/payflow/internal/workerpool"
)

// version - версия сборки для payflow_build_info. При сборке образа
// перезаписывается через -ldflags "-X main.version=...".
var version = "dev"

const (
	workers         = 20
	queueCapacity   = 100
	acquirerTimeout = 5 * time.Second
	// Бюджет остановки: API (10 с) + пул (15 с) + служебный сервер (2 с)
	// < 30 с, которые Kubernetes даёт поду между SIGTERM и SIGKILL.
	httpShutdownTimeout    = 10 * time.Second
	poolStopTimeout        = 15 * time.Second
	metricsShutdownTimeout = 2 * time.Second
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

	// ctx отменяется по SIGINT (Ctrl+C), SIGTERM (docker stop, Kubernetes)
	// или при сбое одного из серверов. Почему именно, скажет context.Cause:
	// NotifyContext кладёт туда сигнал, серверы - свою ошибку.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancelCause(sigCtx)
	defer cancel(nil)
	context.AfterFunc(ctx, func() {
		logger.Info("shutdown requested", zap.NamedError("reason", context.Cause(ctx)))
	})

	reg, err := metrics.NewRegistry(version)
	if err != nil {
		return fmt.Errorf("init metrics: %w", err)
	}
	httpMetrics, err := middleware.NewHTTPMetrics(reg)
	if err != nil {
		return fmt.Errorf("init http metrics: %w", err)
	}

	// Порты открываем первыми: если какой-то занят, выходим до запуска
	// горутин. Адрес в ошибку добавлять не нужно, его уже пишет net:
	// "listen tcp :8081: bind: address already in use".
	var lc net.ListenConfig
	apiLn, err := lc.Listen(ctx, "tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen api: %w", err)
	}
	metricsLn, err := lc.Listen(ctx, "tcp", cfg.metricsAddr)
	if err != nil {
		_ = apiLn.Close()
		return fmt.Errorf("listen metrics: %w", err)
	}

	repo := memory.NewPaymentRepository()
	idempotency := memory.NewIdempotencyStore(idempotencyKeyTTL)

	bank := acquirer.NewFake(acquirer.DefaultFakeConfig())
	processor := service.NewProcessor(repo, bank, acquirerTimeout, logger)
	pool := workerpool.New(workers, queueCapacity, processor.Process, logger)

	go idempotency.RunCleanup(ctx, idempotencyCleanupInterval)

	svc := service.NewPaymentService(repo, idempotency, pool, logger)
	handler := router.New(logger, httpMetrics, httptransport.NewPaymentHandler(svc, logger))

	// У служебного сервера свой контекст, а не ctx: он должен пережить
	// остановку API и пула, чтобы Prometheus видел, как дорабатывается
	// очередь. Отменяем его сами, последним.
	metricsCtx, stopMetrics := context.WithCancel(context.Background())
	defer stopMetrics()
	metricsCfg := server.DefaultConfig()
	metricsCfg.ShutdownTimeout = metricsShutdownTimeout
	metricsErr := make(chan error, 1)
	go func() {
		err := server.Run(metricsCtx, metricsLn, newMetricsMux(metrics.Handler(reg, logger)),
			metricsCfg, logger.With(zap.String("server", "metrics")))
		if err != nil {
			// Без метрик сервис работает вслепую: останавливаем его целиком.
			cancel(fmt.Errorf("metrics server: %w", err))
		}
		metricsErr <- err
	}()

	apiErr := server.Run(ctx, apiLn, handler, cfg.server, logger.With(zap.String("server", "api")))
	if apiErr != nil {
		// API упал сам, без сигнала: фиксируем причину. Если ctx уже
		// отменён, cancel ничего не меняет и причина остаётся прежней.
		cancel(fmt.Errorf("api server: %w", apiErr))
	}

	// API уже не принимает запросы - значит, новых платежей не будет.
	// Теперь дорабатываем очередь. Если Shutdown не уложился в таймаут,
	// оставшиеся обработчики ещё могут дойти до Enqueue: пул вернёт им
	// ErrStopped, и сервис запишет это в лог.
	stopCtx, cancelStop := context.WithTimeout(context.Background(), poolStopTimeout)
	defer cancelStop()
	if err := pool.Stop(stopCtx); err != nil {
		logger.Error("worker pool stopped before queue was drained", zap.Error(err))
	} else {
		logger.Info("worker pool stopped")
	}

	// Служебный сервер - последним.
	stopMetrics()
	return errors.Join(apiErr, <-metricsErr)
}

// newMetricsMux - маршруты служебного сервера: только GET /metrics.
// Другие пути получают 404, другие методы на /metrics - 405.
func newMetricsMux(metricsHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metricsHandler)
	return mux
}
