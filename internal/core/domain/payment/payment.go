// Package paymentdomain contains payment domain types.
package paymentdomain

import (
	"time"

	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

// Payment - платёж. Создавайте его через NewPayment: он проверяет инварианты.
type Payment struct {
	ID            string
	MerchantID    string
	AmountMinor   int64
	Currency      string
	Status        Status
	FailureReason string // только для declined и failed
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

var supportedCurrencies = map[string]struct{}{
	"RUB": {},
	"USD": {},
	"EUR": {},
}

// NewPayment создаёт платёж после проверки данных. Все ошибки оборачивают
// paymenterrors.ErrValidation.
func NewPayment(id string, merchantID string, amountMinor int64, currency string, now time.Time) (Payment, error) {
	if merchantID == "" {
		return Payment{}, paymenterrors.ErrEmptyMerchantID
	}

	if amountMinor <= 0 {
		return Payment{}, paymenterrors.ErrInvalidAmount
	}

	if currency == "" {
		return Payment{}, paymenterrors.ErrEmptyCurrency
	}

	if _, ok := supportedCurrencies[currency]; !ok {
		return Payment{}, paymenterrors.ErrUnsupportedCurrency
	}
	now = now.UTC()

	return Payment{
		ID:          id,
		MerchantID:  merchantID,
		AmountMinor: amountMinor,
		Currency:    currency,
		Status:      StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}
