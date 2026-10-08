package paymentdomain

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cagitic1-source/payflow/internal/core/errors/paymenterrors"
)

func TestPaymentTransitions(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)

	statuses := []Status{StatusPending, StatusProcessing, StatusApproved, StatusDeclined, StatusFailed}

	// Ожидания записаны вручную, а не взяты из allowedTransitions: иначе тест
	// повторял бы таблицу и не заметил бы ошибку в ней.
	allowed := map[[2]Status]bool{
		{StatusPending, StatusProcessing}:  true,
		{StatusProcessing, StatusApproved}: true,
		{StatusProcessing, StatusDeclined}: true,
		{StatusProcessing, StatusFailed}:   true,
	}

	// Как вызвать переход в каждый целевой статус. В pending перейти нельзя - метода нет.
	apply := map[Status]func(p *Payment) error{
		StatusProcessing: func(p *Payment) error { return p.StartProcessing(later) },
		StatusApproved:   func(p *Payment) error { return p.Approve(later) },
		StatusDeclined:   func(p *Payment) error { return p.Decline(later, "insufficient_funds") },
		StatusFailed:     func(p *Payment) error { return p.Fail(later, "acquirer_timeout") },
	}

	for _, from := range statuses {
		for to, do := range apply {
			t.Run(fmt.Sprintf("%s->%s", from, to), func(t *testing.T) {
				p := Payment{Status: from, CreatedAt: now, UpdatedAt: now}
				before := p

				err := do(&p)

				if allowed[[2]Status{from, to}] {
					if err != nil {
						t.Fatalf("want allowed, got %v", err)
					}
					if p.Status != to || !p.UpdatedAt.Equal(later) {
						t.Errorf("got status %s updated %v, want %s %v", p.Status, p.UpdatedAt, to, later)
					}
					return
				}

				if !errors.Is(err, paymenterrors.ErrInvalidTransition) {
					t.Fatalf("want ErrInvalidTransition, got %v", err)
				}
				if p != before {
					t.Errorf("payment changed on rejected transition: %+v", p)
				}
			})
		}
	}
}

func TestPaymentFinalTransitionsKeepReasonAndCreatedAt(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)

	tests := []struct {
		name       string
		do         func(p *Payment) error
		wantStatus Status
		wantReason string
	}{
		{"approve", func(p *Payment) error { return p.Approve(later) }, StatusApproved, ""},
		{"decline", func(p *Payment) error { return p.Decline(later, "insufficient_funds") }, StatusDeclined, "insufficient_funds"},
		{"fail", func(p *Payment) error { return p.Fail(later, "acquirer_timeout") }, StatusFailed, "acquirer_timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Payment{Status: StatusProcessing, CreatedAt: now, UpdatedAt: now}

			if err := tt.do(&p); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Status != tt.wantStatus || p.FailureReason != tt.wantReason {
				t.Errorf("got (%s, %q), want (%s, %q)", p.Status, p.FailureReason, tt.wantStatus, tt.wantReason)
			}
			if !p.CreatedAt.Equal(now) {
				t.Errorf("created_at changed: %v", p.CreatedAt)
			}
		})
	}
}

func TestNewPayment(t *testing.T) {
	// Не UTC - проверяем, что NewPayment приводит время к UTC.
	now := time.Date(2026, 10, 5, 15, 0, 0, 0, time.FixedZone("MSK", 3*60*60))

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
			p, err := NewPayment(tt.id, tt.merchantID, tt.amountMinor, tt.currency, now)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("want error %v, got %v", tt.wantErr, err)
				}

				if !errors.Is(err, paymenterrors.ErrValidation) {
					t.Errorf("error %v must wrap ErrValidation", err)
				}
				if p != (Payment{}) {
					t.Errorf("want zero Payment on error, got %+v", p)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := Payment{
				ID:          tt.id,
				MerchantID:  tt.merchantID,
				AmountMinor: tt.amountMinor,
				Currency:    tt.currency,
				Status:      StatusPending,
				CreatedAt:   now.UTC(),
				UpdatedAt:   now.UTC(),
			}
			if p != want {
				t.Errorf("got %+v, want %+v", p, want)
			}
		})
	}
}
