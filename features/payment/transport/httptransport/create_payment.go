// Package httptransport contains HTTP transport implementations for the payment domain.
package httptransport

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/internal/core/errors/paymenterrors"
)

// CreatePayment обрабатывает POST /v1/payments: создаёт платёж и отвечает 202
// с телом платежа и заголовком Location. Невалидное тело, тело больше
// maxRequestBodyBytes или нет заголовка Idempotency-Key - 400, ошибки валидации - 422. Повтор с тем же ключом
// и телом получает тот же ответ с заголовком Idempotent-Replayed: true;
// пока первый запрос выполняется - 409, ключ с другим телом - 422.
func (h *PaymentHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get(idempotencyKeyHeader)
	if key == "" || len(key) > 255 {
		respondError(w, r, h.log, paymenterrors.ErrEmptyIdempotencyKey)
		return
	}

	var req createPaymentRequest

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		respondError(w, r, h.log, fmt.Errorf("%w: %w", ErrMalformedRequest, err))
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		respondError(w, r, h.log, fmt.Errorf("%w: unexpected data after JSON object", ErrMalformedRequest))
		return
	}

	cmd := req.toCommand()
	cmd.IdempotencyKey = key
	res, err := h.svc.CreatePayment(r.Context(), cmd)
	if err != nil {
		respondError(w, r, h.log, err)
		return
	}

	log := requestLogger(r, h.log)
	log.Info("payment accepted",
		zap.String("payment_id", res.Payment.ID),
		zap.Bool("replayed", res.Replayed))

	if res.Replayed {
		w.Header().Set(IdempotentReplayedHeader, "true")
	}
	w.Header().Set("Location", "/v1/payments/"+res.Payment.ID)
	writeJSON(w, h.log, http.StatusAccepted, toPaymentResponse(res.Payment))
}
