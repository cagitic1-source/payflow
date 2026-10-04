package service_test

import (
	"errors"
	"testing"

	"github.com/cagitic1-source/payflow/features/payment/service"

	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

func TestGetPayment_EmptyID(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

	_, err := svc.GetPayment(t.Context(), "")
	if err == nil {
		t.Fatal("expected error for empty ID, got nil")
	}
	if !errors.Is(err, paymenterrors.ErrEmptyPaymentID) {
		t.Fatalf("expected ErrEmptyPaymentID, got %v", err)
	}
}

func TestGetPayment_Found(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

	created, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	got, err := svc.GetPayment(t.Context(), created.Payment.ID)
	if err != nil {
		t.Fatalf("get payment: %v", err)
	}
	if got != created.Payment {
		t.Errorf("got %+v, want %+v", got, created)
	}
}

func TestGetPayment_NotFound(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{})

	_, err := svc.GetPayment(t.Context(), "11111")
	if !errors.Is(err, paymenterrors.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

}
