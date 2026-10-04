// Command payment-api запускает HTTP API платёжного сервиса.
package main

import (
	"log"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/payment/memory"
	"github.com/cagitic1-source/payflow/features/payment/service"
	"github.com/cagitic1-source/payflow/features/payment/transport/httptransport"
	"github.com/cagitic1-source/payflow/internal/app/http/v1/router"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()

	paymentRepo := memory.NewPaymentRepository()
	paymentService := service.NewPaymentService(paymentRepo)
	paymentTransport := httptransport.NewPaymentHandler(paymentService, logger)

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           router.New(logger, paymentTransport),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Println("Запуск HTTP сервера")
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("HTTP server error: %v", err)
		return

	}

}
