package service_test

import (
	"errors"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	"github.com/cagitic1-source/payflow/internal/core/errors/paymenterrors"
	"github.com/cagitic1-source/payflow/internal/core/requestctx"
)

func TestCreatePayment_EnqueuesSavedPayment(t *testing.T) {
	repo, queue := &fakeRepo{}, &fakeQueue{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{}, queue, zap.NewNop())

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
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{}, queue, zap.NewNop())

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
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{}, queue, zap.NewNop())

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
	svc := service.NewPaymentService(&fakeRepo{}, &fakeIdempotencyStore{}, queue, zap.NewNop())

	if _, err := svc.CreatePayment(t.Context(), command("k1", 100)); err != nil {
		t.Fatalf("first: %v", err)
	}
	queue.full = true // мест больше нет, но повтору место и не нужно
	res, err := svc.CreatePayment(t.Context(), command("k1", 100))
	if err != nil || !res.Replayed {
		t.Fatalf("replay must succeed without a slot, got (%+v, %v)", res, err)
	}
}

// Пул остановили между TryAcquire и Enqueue. Платёж уже сохранён, поэтому
// он считается принятым: ключ завершается, а ошибка попадает в лог запроса
// с payment_id - после рестарта такой pending подберёт восстановление.
func TestCreatePayment_NotEnqueuedIsLoggedAndAccepted(t *testing.T) {
	repo, queue := &fakeRepo{}, &fakeQueue{stopped: true}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{}, queue, zap.NewNop())

	core, logs := observer.New(zap.ErrorLevel)
	ctx := requestctx.WithLogger(t.Context(), zap.New(core).With(zap.String("request_id", "req-1")))

	res, err := svc.CreatePayment(ctx, command("k1", 100))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(repo.saved) != 1 {
		t.Errorf("saved %d payments, want 1", len(repo.saved))
	}
	// Место вернула сама очередь. Release из сервиса освободил бы чужое.
	if queue.acquired != 0 || queue.releaseCalls != 0 {
		t.Errorf("acquired %d, release calls %d, want 0 and 0", queue.acquired, queue.releaseCalls)
	}

	entries := logs.FilterMessage("payment saved but not enqueued").All()
	if len(entries) != 1 {
		t.Fatalf("want one error entry, got %+v", logs.All())
	}
	fields := entries[0].ContextMap()
	if fields["payment_id"] != res.Payment.ID || fields["request_id"] != "req-1" {
		t.Errorf("want payment_id=%s and request_id=req-1, got %v", res.Payment.ID, fields)
	}

	// Ключ завершён: повтор возвращает тот же платёж, а не создаёт новый.
	again, err := svc.CreatePayment(t.Context(), command("k1", 100))
	if err != nil || !again.Replayed || again.Payment.ID != res.Payment.ID {
		t.Errorf("retry must replay %s, got (%+v, %v)", res.Payment.ID, again, err)
	}
}
