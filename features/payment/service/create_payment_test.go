// Package service_test contains tests for the payment service.
package service_test

import (
	"errors"
	"testing"

	"github.com/cagitic1-source/payflow/features/payment/service"
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

func TestCreatePayment_EmptyIdempotencyKey(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

	cmd := validPaymentCommand()
	cmd.IdempotencyKey = ""
	_, err := svc.CreatePayment(t.Context(), cmd)
	if !errors.Is(err, paymenterrors.ErrEmptyIdempotencyKey) {
		t.Fatalf("want ErrEmptyIdempotencyKey, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Fatalf("want nothing saved, got %d payments", len(repo.saved))
	}
}

func TestCreatePayment_Replay(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

	first, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if first.Replayed {
		t.Fatal("first create must not be marked as replayed")
	}

	second, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err != nil {
		t.Fatalf("replayed create: %v", err)
	}
	if !second.Replayed {
		t.Error("second create with the same key must be marked as replayed")
	}
	if second.Payment != first.Payment {
		t.Errorf("replay returned %+v, want %+v", second.Payment, first.Payment)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("want 1 payment saved, got %d", len(repo.saved))
	}
}

func TestCreatePayment_KeyReusedWithDifferentBody(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

	if _, err := svc.CreatePayment(t.Context(), validPaymentCommand()); err != nil {
		t.Fatalf("first create: %v", err)
	}

	cmd := validPaymentCommand()
	cmd.AmountMinor = 2000
	_, err := svc.CreatePayment(t.Context(), cmd)
	if !errors.Is(err, paymenterrors.ErrIdempotencyKeyReused) {
		t.Fatalf("want ErrIdempotencyKeyReused, got %v", err)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("want 1 payment saved, got %d", len(repo.saved))
	}
}

func TestCreatePayment_KeysScopedByMerchant(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

	p1, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err != nil {
		t.Fatalf("create for merchant1: %v", err)
	}

	// Тот же ключ у другого мерчанта — это другой запрос, а не повтор.
	cmd := validPaymentCommand()
	cmd.MerchantID = "merchant2"
	p2, err := svc.CreatePayment(t.Context(), cmd)
	if err != nil {
		t.Fatalf("create for merchant2: %v", err)
	}
	if p2.Replayed || p2.Payment.ID == p1.Payment.ID {
		t.Fatalf("merchant2 got merchant1's payment: %+v", p2)
	}
}

func TestCreatePayment_RetryAfterSaveError(t *testing.T) {
	repo := &fakeRepo{saveErr: errors.New("db is down")}
	idem := &fakeIdempotencyStore{}
	svc := service.NewPaymentService(repo, idem)

	if _, err := svc.CreatePayment(t.Context(), validPaymentCommand()); err == nil {
		t.Fatal("want error from failing repository, got nil")
	}
	if idem.releaseCalls != 1 {
		t.Fatalf("want key released once after save error, got %d Release calls", idem.releaseCalls)
	}

	// Ключ освобождён после сбоя — повтор создаёт платёж, а не получает 409.
	repo.saveErr = nil
	p, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err != nil {
		t.Fatalf("retry after save error: %v", err)
	}
	if p.Replayed {
		t.Error("retry after failure must not be marked as replayed")
	}
	if len(repo.saved) != 1 {
		t.Fatalf("want 1 payment saved, got %d", len(repo.saved))
	}
}

func TestCreatePayment_ReserveError(t *testing.T) {
	repo := &fakeRepo{}
	idem := &fakeIdempotencyStore{reserveErr: paymenterrors.ErrIdempotencyInProgress}
	svc := service.NewPaymentService(repo, idem)

	_, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if !errors.Is(err, paymenterrors.ErrIdempotencyInProgress) {
		t.Fatalf("want ErrIdempotencyInProgress, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Fatalf("want nothing saved, got %d payments", len(repo.saved))
	}
	// Ключ занят другим запросом — освобождать его нельзя.
	if idem.releaseCalls != 0 {
		t.Fatalf("want no Release for a key we did not reserve, got %d calls", idem.releaseCalls)
	}
}

func TestCreatePayment_CompleteError(t *testing.T) {
	errStore := errors.New("store is down")
	repo := &fakeRepo{}
	idem := &fakeIdempotencyStore{completeErr: errStore}
	svc := service.NewPaymentService(repo, idem)

	_, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if !errors.Is(err, errStore) {
		t.Fatalf("want %v, got %v", errStore, err)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("want 1 payment saved, got %d", len(repo.saved))
	}
	// Платёж уже сохранён: освобождённый ключ позволил бы повтору создать второй.
	if idem.releaseCalls != 0 {
		t.Fatalf("want no Release after payment is saved, got %d calls", idem.releaseCalls)
	}
}
