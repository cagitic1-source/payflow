// Внутренний тест: проверяет неэкспортируемую таблицу problemSpecs.
package httptransport

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

func TestProblemFromError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantType   string
		wantField  string
	}{
		// --- по строке на каждую запись таблицы ---
		{"empty merchant", paymenterrors.ErrEmptyMerchantID, http.StatusUnprocessableEntity, problemTypeBase + "empty-merchant-id", "merchant_id"},
		{"invalid amount", paymenterrors.ErrInvalidAmount, http.StatusUnprocessableEntity, problemTypeBase + "invalid-amount", "amount_minor"},
		{"empty currency", paymenterrors.ErrEmptyCurrency, http.StatusUnprocessableEntity, problemTypeBase + "empty-currency", "currency"},
		{"unsupported currency", paymenterrors.ErrUnsupportedCurrency, http.StatusUnprocessableEntity, problemTypeBase + "unsupported-currency", "currency"},
		{"empty payment id", paymenterrors.ErrEmptyPaymentID, http.StatusUnprocessableEntity, problemTypeBase + "empty-payment-id", ""},
		{"not found", paymenterrors.ErrNotFound, http.StatusNotFound, problemTypeBase + "payment-not-found", ""},
		{"malformed request", fmt.Errorf("%w: unexpected EOF", ErrMalformedRequest), http.StatusBadRequest, problemTypeBase + "malformed-request", ""},
		{"missing idempotency key", paymenterrors.ErrEmptyIdempotencyKey, http.StatusBadRequest, problemTypeBase + "missing-idempotency-key", ""},
		{"idempotency key reused", paymenterrors.ErrIdempotencyKeyReused, http.StatusUnprocessableEntity, problemTypeBase + "idempotency-key-reused", ""},
		{"idempotency in progress", fmt.Errorf("reserve idempotency key: %w", paymenterrors.ErrIdempotencyInProgress), http.StatusConflict, problemTypeBase + "idempotency-request-in-progress", ""},

		// --- поведение маппинга ---
		{"wrapped error is found", fmt.Errorf("get payment: %w", paymenterrors.ErrNotFound), http.StatusNotFound, problemTypeBase + "payment-not-found", ""},
		{"validation error without own row", fmt.Errorf("%w: test", paymenterrors.ErrValidation), http.StatusUnprocessableEntity, problemTypeBase + "validation-error", ""},
		{"unknown error becomes 500", errors.New("db is down"), http.StatusInternalServerError, "about:blank", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := problemFromError(tt.err)

			if p.Status != tt.wantStatus {
				t.Errorf("status: got %d, want %d", p.Status, tt.wantStatus)
			}
			if p.Type != tt.wantType {
				t.Errorf("type: got %q, want %q", p.Type, tt.wantType)
			}
			if p.Field != tt.wantField {
				t.Errorf("field: got %q, want %q", p.Field, tt.wantField)
			}
		})
	}
}

// Каждая строка таблицы должна быть достижима: её ошибка должна
// находить именно её, а не строку выше. Ловит неправильный порядок,
// даже если для новой строки забыли добавить случай в тест выше.
func TestProblemSpecsAllReachable(t *testing.T) {
	for i, spec := range problemSpecs {
		got := problemFromError(spec.err)
		if got.Type != problemTypeBase+spec.slug {
			t.Errorf("row %d (%s) is shadowed: error resolves to %q", i, spec.slug, got.Type)
		}
	}
}
