// Package payment contains payment domain types.
package payment

type Payment struct {
	ID          string
	MerchantID  string
	AmountMinor int64
	Currency    string
}

type CreatePaymentRequest struct {
	MerchantID string `json:"merchant_id"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
}
