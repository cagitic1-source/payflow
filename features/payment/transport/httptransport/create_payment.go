// Package httptransport contains HTTP transport implementations for the payment domain.
package httptransport

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CreatePayment обрабатывает POST /v1/payments: создаёт платёж и отвечает 201
// с телом платежа и заголовком Location. Невалидное тело — 400, ошибки
// валидации — 422.
func (h *PaymentHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	var req createPaymentRequest

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
	p, err := h.svc.CreatePayment(r.Context(), cmd)
	if err != nil {
		respondError(w, r, h.log, err)
		return
	}

	w.Header().Set("Location", "/v1/payments/"+p.ID)
	writeJSON(w, h.log, http.StatusCreated, toPaymentResponse(p))
}
