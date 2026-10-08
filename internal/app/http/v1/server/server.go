// Package server runs the HTTP server with timeouts and graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Config - таймауты HTTP-сервера. Значения по умолчанию даёт DefaultConfig.
type Config struct {
	ReadHeaderTimeout time.Duration // заголовки запроса; защита от Slowloris
	ReadTimeout       time.Duration // весь запрос вместе с телом
	WriteTimeout      time.Duration // запись ответа
	IdleTimeout       time.Duration // простой keep-alive соединения
	ShutdownTimeout   time.Duration // сколько ждать активные запросы при остановке
}

// DefaultConfig возвращает разумные значения по умолчанию.
// ShutdownTimeout меньше 30 с - стандартного времени, которое Kubernetes
// даёт поду между SIGTERM и принудительным SIGKILL.
func DefaultConfig() Config {
	return Config{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   20 * time.Second,
	}
}

// Run обслуживает запросы на ln, пока не отменят ctx, затем корректно
// останавливает сервер: новые соединения не принимаются, а начатые запросы
// получают cfg.ShutdownTimeout на завершение. Возвращает ошибку, если сервер
// упал сам или не успел остановиться за это время.
func Run(ctx context.Context, ln net.Listener, handler http.Handler, cfg Config, log *zap.Logger) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	serveErr := make(chan error, 1)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		log.Info("http server started", zap.String("addr", ln.Addr().String()))
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down http server")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()

	if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil {
		if err := srv.Close(); err != nil {
			return fmt.Errorf("server closed and failed shutdown: %w", err)
		}
		return fmt.Errorf("shutdown: %w", shutdownErr)
	}

	// Дожидаемся выхода из Serve. Если ctx отменили раньше, чем горутина
	// успела вызвать Serve, Shutdown вернётся сразу, а listener закроется
	// только когда Serve всё-таки запустится и увидит, что сервер остановлен.
	<-serveDone

	log.Info("http server stopped")
	return nil
}
