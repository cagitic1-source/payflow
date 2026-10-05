package httptransport

import (
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
)

type createPaymentRequest struct {
	MerchantID  string `json:"merchant_id"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

func (r createPaymentRequest) toCommand() service.CreatePaymentCommand {
	return service.CreatePaymentCommand{
		MerchantID:  r.MerchantID,
		AmountMinor: r.AmountMinor,
		Currency:    r.Currency,
	}
}

type paymentResponse struct {
	ID            string    `json:"id"`
	MerchantID    string    `json:"merchant_id"`
	AmountMinor   int64     `json:"amount_minor"`
	Currency      string    `json:"currency"`
	Status        string    `json:"status"`
	FailureReason string    `json:"failure_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func toPaymentResponse(p paymentdomain.Payment) paymentResponse {
	return paymentResponse{
		ID:            p.ID,
		MerchantID:    p.MerchantID,
		AmountMinor:   p.AmountMinor,
		Currency:      p.Currency,
		Status:        string(p.Status),
		FailureReason: p.FailureReason,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

func writeJSON(w http.ResponseWriter, log *zap.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Debug("write JSON response", zap.Error(err))
	}
}
