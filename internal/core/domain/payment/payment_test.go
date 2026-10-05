package paymentdomain_test

import (
	"errors"
	"testing"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

func TestNewPayment(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		merchantID  string
		amountMinor int64
		currency    string
		wantErr     error // nil - ошибки быть не должно
	}{
		{name: "valid RUB", id: "id-1", merchantID: "m-1", amountMinor: 100, currency: "RUB"},
		{name: "valid USD", id: "id-2", merchantID: "m-2", amountMinor: 1, currency: "USD"},
		{name: "empty merchant", id: "id-1", merchantID: "", amountMinor: 100, currency: "RUB", wantErr: paymenterrors.ErrEmptyMerchantID},
		{name: "zero amount", id: "id-1", merchantID: "m-1", amountMinor: 0, currency: "RUB", wantErr: paymenterrors.ErrInvalidAmount},
		{name: "negative amount", id: "id-1", merchantID: "m-1", amountMinor: -100, currency: "RUB", wantErr: paymenterrors.ErrInvalidAmount},
		{name: "empty currency", id: "id-1", merchantID: "m-1", amountMinor: 100, currency: "", wantErr: paymenterrors.ErrEmptyCurrency},
		{name: "unsupported currency", id: "id-1", merchantID: "m-1", amountMinor: 100, currency: "XXX", wantErr: paymenterrors.ErrUnsupportedCurrency},
		{name: "lowercase currency", id: "id-1", merchantID: "m-1", amountMinor: 100, currency: "rub", wantErr: paymenterrors.ErrUnsupportedCurrency},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := paymentdomain.NewPayment(tt.id, tt.merchantID, tt.amountMinor, tt.currency)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("want error %v, got %v", tt.wantErr, err)
				}

				if !errors.Is(err, paymenterrors.ErrValidation) {
					t.Errorf("error %v must wrap ErrValidation", err)
				}
				if p != (paymentdomain.Payment{}) {
					t.Errorf("want zero Payment on error, got %+v", p)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := paymentdomain.Payment{
				ID:          tt.id,
				MerchantID:  tt.merchantID,
				AmountMinor: tt.amountMinor,
				Currency:    tt.currency,
			}
			if p != want {
				t.Errorf("got %+v, want %+v", p, want)
			}
		})
	}
}
