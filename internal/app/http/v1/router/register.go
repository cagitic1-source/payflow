// Package router assembles the v1 HTTP API.
package router

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/payment/transport/httptransport"
	"github.com/cagitic1-source/payflow/internal/app/http/middleware"
)

// New собирает HTTP-обработчик API v1: маршруты /healthz и /v1/payments,
// обёрнутые в middleware RequestID и Recover.
func New(log *zap.Logger, paymentHandler *httptransport.PaymentHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthzHandler)
	registerPayments(mux, paymentHandler)

	return middleware.RequestID(log, middleware.Recover(log, mux))
}

func registerPayments(mux *http.ServeMux, p *httptransport.PaymentHandler) {
	mux.HandleFunc("POST /v1/payments", p.CreatePayment)
	mux.HandleFunc("GET /v1/payments/{id}", p.GetPayment)

}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok"))
}
