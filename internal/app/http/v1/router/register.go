// Package router assembles the v1 HTTP API.
package router

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/payment/transport/httptransport"
	"github.com/cagitic1-source/payflow/internal/app/http/middleware"
)

// maxRequestBodyBytes - предел тела любого запроса к API. Тело платежа -
// около сотни байт, 1 MiB - большой запас и прежний лимит из README.
const maxRequestBodyBytes = 1 << 20

// New собирает HTTP-обработчик API v1: маршруты /healthz и /v1/payments,
// обёрнутые в middleware BodyLimit, RequestID, метрики и Recover.
func New(log *zap.Logger, httpMetrics *middleware.HTTPMetrics, paymentHandler *httptransport.PaymentHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthzHandler)
	registerPayments(mux, paymentHandler)

	// Порядок важен, снаружи внутрь:
	//   - BodyLimit первым: http.MaxBytesReader должен получить исходный
	//     ResponseWriter, иначе сервер не узнает о превышении лимита и не
	//     закроет соединение после ответа;
	//   - метрики внутри RequestID: им нужен r.Pattern от mux, а RequestID
	//     копирует запрос;
	//   - метрики снаружи Recover: так они видят 500, который пишет Recover.
	h := middleware.Recover(log, mux)
	h = httpMetrics.Middleware(h)
	h = middleware.RequestID(log, h)
	h = middleware.BodyLimit(maxRequestBodyBytes, h)
	return h
}

func registerPayments(mux *http.ServeMux, p *httptransport.PaymentHandler) {
	mux.HandleFunc("POST /v1/payments", p.CreatePayment)
	mux.HandleFunc("GET /v1/payments/{id}", p.GetPayment)
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok"))
}
