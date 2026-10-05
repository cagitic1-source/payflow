// Package service_test contains tests for the payment service.
package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

func TestCreatePayment_Success(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

	p, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err != nil {
		t.Fatalf("failed to create payment: %v", err)
	}
	if p.Payment.ID == "" {
		t.Fatal("expected non-empty payment ID")
	}
	if p.Payment.MerchantID != "merchant1" {
		t.Fatalf("expected MerchantID 'merchant1', got '%s'", p.Payment.MerchantID)
	}
	if p.Payment.AmountMinor != 1000 {
		t.Fatalf("expected AmountMinor 1000, got %d", p.Payment.AmountMinor)
	}
	if p.Payment.Currency != "USD" {
		t.Fatalf("expected Currency 'USD', got '%s'", p.Payment.Currency)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("expected 1 payment saved, got %d", len(repo.saved))
	}
	if repo.saved[0].ID != p.Payment.ID {
		t.Fatalf("expected saved payment ID %s, got %s", p.Payment.ID, repo.saved[0].ID)
	}
}

func TestCreatePayment_UsesServiceClock(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})
	svc.SetNow(func() time.Time { return now })

	p, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err != nil {
		t.Fatalf("failed to create payment: %v", err)
	}
	if p.Payment.Status != paymentdomain.StatusPending {
		t.Errorf("want status %q, got %q", paymentdomain.StatusPending, p.Payment.Status)
	}
	if !p.Payment.CreatedAt.Equal(now) || !p.Payment.UpdatedAt.Equal(now) {
		t.Errorf("want CreatedAt and UpdatedAt %v, got %v and %v", now, p.Payment.CreatedAt, p.Payment.UpdatedAt)
	}
	if len(repo.saved) != 1 || !repo.saved[0].CreatedAt.Equal(now) {
		t.Errorf("saved payment must carry CreatedAt %v, got %+v", now, repo.saved)
	}
}

func TestCreatePayment_RepositoryError(t *testing.T) {
	errDB := errors.New("db is down")
	repo := &fakeRepo{saveErr: errDB}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

	_, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errDB) {
		t.Fatalf("expected error %v, got %v", errDB, err)
	}
}

func TestCreatePayment_UniqueIDs(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

	p1, err1 := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err1 != nil {
		t.Fatalf("failed to create first payment: %v", err1)
	}

	cmd2 := validPaymentCommand()
	cmd2.IdempotencyKey = "key-2" // с тем же ключом это был бы повтор
	p2, err2 := svc.CreatePayment(t.Context(), cmd2)
	if err2 != nil {
		t.Fatalf("failed to create second payment: %v", err2)
	}

	if p1.Payment.ID == p2.Payment.ID {
		t.Fatal("expected unique payment IDs, got same ID")
	}
}

func TestCreatePayment_ValidationError(t *testing.T) {
	tests := []struct {
		name    string
		cmd     service.CreatePaymentCommand
		wantErr error
	}{
		{
			name:    "empty merchant",
			cmd:     service.CreatePaymentCommand{MerchantID: "", AmountMinor: 1000, Currency: "USD", IdempotencyKey: "key-1"},
			wantErr: paymenterrors.ErrEmptyMerchantID,
		},
		{
			name:    "zero amount",
			cmd:     service.CreatePaymentCommand{MerchantID: "merchant1", AmountMinor: 0, Currency: "USD", IdempotencyKey: "key-1"},
			wantErr: paymenterrors.ErrInvalidAmount,
		},
		{
			name:    "negative amount",
			cmd:     service.CreatePaymentCommand{MerchantID: "merchant1", AmountMinor: -100, Currency: "USD", IdempotencyKey: "key-1"},
			wantErr: paymenterrors.ErrInvalidAmount,
		},
		{
			name:    "empty currency",
			cmd:     service.CreatePaymentCommand{MerchantID: "merchant1", AmountMinor: 1000, Currency: "", IdempotencyKey: "key-1"},
			wantErr: paymenterrors.ErrEmptyCurrency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

			p, err := svc.CreatePayment(t.Context(), tt.cmd)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("want error %v, got %v", tt.wantErr, err)
			}
			if !errors.Is(err, paymenterrors.ErrValidation) {
				t.Errorf("error %v must wrap ErrValidation", err)
			}
			if p != (service.CreatePaymentResult{}) {
				t.Errorf("want zero result on error, got %+v", p)
			}
			if len(repo.saved) != 0 {
				t.Errorf("repository must not be called on validation error, saved %d payments", len(repo.saved))
			}
		})
	}
}
