package service_test

import (
	"errors"
	"testing"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

func TestCreatePayment_EnqueuesSavedPayment(t *testing.T) {
	repo, queue := &fakeRepo{}, &fakeQueue{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{}, queue)

	res, err := svc.CreatePayment(t.Context(), command("k1", 100))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0] != res.Payment.ID {
		t.Errorf("enqueued %v, want [%s]", queue.enqueued, res.Payment.ID)
	}
	if res.Payment.Status != paymentdomain.StatusPending {
		t.Errorf("status %s, want %s", res.Payment.Status, paymentdomain.StatusPending)
	}
}

func TestCreatePayment_OverloadedSavesNothingAndFreesKey(t *testing.T) {
	repo, queue := &fakeRepo{}, &fakeQueue{full: true}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{}, queue)

	_, err := svc.CreatePayment(t.Context(), command("k1", 100))
	if !errors.Is(err, paymenterrors.ErrOverloaded) {
		t.Fatalf("want ErrOverloaded, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Errorf("saved %d payments on overload, want 0", len(repo.saved))
	}

	// Место освободилось, клиент повторяет с тем же ключом.
	queue.full = false
	res, err := svc.CreatePayment(t.Context(), command("k1", 100))
	if err != nil {
		t.Fatalf("retry with same key after overload must succeed, got %v", err)
	}
	if res.Replayed {
		t.Error("retry after overload must create a payment, not replay")
	}
}

func TestCreatePayment_SaveFailureReleasesSlot(t *testing.T) {
	repo, queue := &fakeRepo{saveErr: errors.New("db is down")}, &fakeQueue{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{}, queue)

	if _, err := svc.CreatePayment(t.Context(), command("k1", 100)); err == nil {
		t.Fatal("want save error, got nil")
	}
	if queue.acquired != 0 {
		t.Errorf("slot leaked: %d acquired after save failure, want 0", queue.acquired)
	}
	if len(queue.enqueued) != 0 {
		t.Errorf("enqueued %v after save failure, want nothing", queue.enqueued)
	}
}

func TestCreatePayment_ReplayDoesNotTakeSlot(t *testing.T) {
	queue := &fakeQueue{}
	svc := service.NewPaymentService(&fakeRepo{}, &fakeIdempotencyStore{}, queue)

	if _, err := svc.CreatePayment(t.Context(), command("k1", 100)); err != nil {
		t.Fatalf("first: %v", err)
	}
	queue.full = true // мест больше нет, но повтору место и не нужно
	res, err := svc.CreatePayment(t.Context(), command("k1", 100))
	if err != nil || !res.Replayed {
		t.Fatalf("replay must succeed without a slot, got (%+v, %v)", res, err)
	}
}
