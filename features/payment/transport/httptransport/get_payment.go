package httptransport

import (
	"net/http"
)

// GetPayment обрабатывает GET /v1/payments/{id}.
func (h *PaymentHandler) GetPayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	p, err := h.svc.GetPayment(r.Context(), id)
	if err != nil {
		respondError(w, r, h.log, err)
		return
	}

	writeJSON(w, h.log, http.StatusOK, toPaymentResponse(p))
}
