package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cagitic1-source/payflow/features/payment/memory"
	"github.com/cagitic1-source/payflow/features/payment/service"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

func command(key string, amount int64) service.CreatePaymentCommand {
	return service.CreatePaymentCommand{MerchantID: "m-1", AmountMinor: amount, Currency: "RUB", IdempotencyKey: key}
}

func TestCreatePayment_EmptyIdempotencyKey(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{}, &fakeQueue{})

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
	idem := &fakeIdempotencyStore{}
	svc := service.NewPaymentService(repo, idem, &fakeQueue{})

	first, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if first.Replayed {
		t.Fatal("first create must not be marked as replayed")
	}
	// После успешного Save флаг saved выставлен - освобождать ключ нельзя.
	if idem.releaseCalls != 0 {
		t.Fatalf("successful create must not release the key, got %d Release calls", idem.releaseCalls)
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
	if idem.releaseCalls != 0 {
		t.Errorf("replay must not release the key, got %d Release calls", idem.releaseCalls)
	}
}

func TestCreatePayment_KeyReusedWithDifferentRequest(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{}, &fakeQueue{})

	if _, err := svc.CreatePayment(t.Context(), command("k1", 100)); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := svc.CreatePayment(t.Context(), command("k1", 999))
	if !errors.Is(err, paymenterrors.ErrIdempotencyKeyReused) {
		t.Fatalf("want ErrIdempotencyKeyReused, got %v", err)
	}
	if len(repo.saved) != 1 {
		t.Errorf("want 1 saved payment, got %d", len(repo.saved))
	}
}

func TestCreatePayment_KeysScopedByMerchant(t *testing.T) {
	repo := &fakeRepo{}
	svc := service.NewPaymentService(repo, &fakeIdempotencyStore{}, &fakeQueue{})

	p1, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if err != nil {
		t.Fatalf("create for merchant1: %v", err)
	}

	// Тот же ключ у другого мерчанта - это другой запрос, а не повтор.
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

func TestCreatePayment_InvalidRequestDoesNotOccupyKey(t *testing.T) {
	svc := service.NewPaymentService(&fakeRepo{}, &fakeIdempotencyStore{}, &fakeQueue{})

	_, err := svc.CreatePayment(t.Context(), command("k1", -5))
	if !errors.Is(err, paymenterrors.ErrValidation) {
		t.Fatalf("invalid request: want ErrValidation, got %v", err)
	}

	// Клиент исправил сумму и повторил с тем же ключом.
	res, err := svc.CreatePayment(t.Context(), command("k1", 100))
	if err != nil {
		t.Fatalf("fixed request with same key must succeed, got %v", err)
	}
	if res.Replayed {
		t.Error("fixed request must create a payment, not replay")
	}
}

func TestCreatePayment_ReserveError(t *testing.T) {
	repo := &fakeRepo{}
	idem := &fakeIdempotencyStore{reserveErr: paymenterrors.ErrIdempotencyInProgress}
	svc := service.NewPaymentService(repo, idem, &fakeQueue{})

	_, err := svc.CreatePayment(t.Context(), validPaymentCommand())
	if !errors.Is(err, paymenterrors.ErrIdempotencyInProgress) {
		t.Fatalf("want ErrIdempotencyInProgress, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Fatalf("want nothing saved, got %d payments", len(repo.saved))
	}
	// Ключ занят другим запросом - освобождать его нельзя.
	if idem.releaseCalls != 0 {
		t.Fatalf("want no Release for a key we did not reserve, got %d calls", idem.releaseCalls)
	}
}

func TestCreatePayment_SaveFailureReleasesKey(t *testing.T) {
	repo := &fakeRepo{saveErr: errors.New("db is down")}
	idem := &fakeIdempotencyStore{}
	svc := service.NewPaymentService(repo, idem, &fakeQueue{})

	if _, err := svc.CreatePayment(t.Context(), command("k1", 100)); err == nil {
		t.Fatal("want save error, got nil")
	}
	if idem.releaseCalls != 1 {
		t.Fatalf("want key released once after save failure, got %d Release calls", idem.releaseCalls)
	}

	repo.saveErr = nil // база ожила, клиент повторяет с тем же ключом
	res, err := svc.CreatePayment(t.Context(), command("k1", 100))
	if err != nil {
		t.Fatalf("retry after save failure must succeed, got %v", err)
	}
	if res.Replayed {
		t.Error("retry after save failure must create a payment, not replay")
	}
	// Успешный повтор ключ не освобождает: счётчик остался прежним.
	if idem.releaseCalls != 1 {
		t.Errorf("successful retry must not release the key, got %d Release calls in total", idem.releaseCalls)
	}
}

func TestCreatePayment_CompleteFailureKeepsKeyReserved(t *testing.T) {
	errStore := errors.New("idempotency storage is down")
	repo := &fakeRepo{}
	idem := &fakeIdempotencyStore{completeErr: errStore}
	svc := service.NewPaymentService(repo, idem, &fakeQueue{})

	if _, err := svc.CreatePayment(t.Context(), command("k1", 100)); !errors.Is(err, errStore) {
		t.Fatalf("want %v, got %v", errStore, err)
	}
	// Платёж уже сохранён: освобождённый ключ позволил бы повтору создать второй.
	if idem.releaseCalls != 0 {
		t.Fatalf("want no Release after payment is saved, got %d calls", idem.releaseCalls)
	}

	// Платёж сохранён, а ключ не завершён. Повтор не должен создать второй платёж.
	_, err := svc.CreatePayment(t.Context(), command("k1", 100))
	if !errors.Is(err, paymenterrors.ErrIdempotencyInProgress) {
		t.Fatalf("want ErrIdempotencyInProgress, got %v", err)
	}
	if len(repo.saved) != 1 {
		t.Errorf("want exactly 1 saved payment (no double charge), got %d", len(repo.saved))
	}
}

func TestCreatePayment_ConcurrentSameKey(t *testing.T) {
	// Здесь настоящий репозиторий: fakeRepo не защищён от конкурентного доступа.
	svc := service.NewPaymentService(memory.NewPaymentRepository(), &fakeIdempotencyStore{}, &fakeQueue{})
	const n = 50

	type result struct {
		res service.CreatePaymentResult
		err error
	}
	results := make(chan result, n)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := svc.CreatePayment(context.Background(), command("k1", 100))
			results <- result{res, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	created := 0
	ids := map[string]bool{}
	for r := range results {
		switch {
		case r.err == nil:
			ids[r.res.Payment.ID] = true
			if !r.res.Replayed {
				created++
			}
		case errors.Is(r.err, paymenterrors.ErrIdempotencyInProgress):
			// допустимо: запрос пришёл, пока первый выполнялся
		default:
			t.Errorf("unexpected error: %v", r.err)
		}
	}

	if created != 1 {
		t.Errorf("want exactly 1 created payment, got %d", created)
	}
	if len(ids) != 1 {
		t.Errorf("all successful responses must carry the same payment, got %d different ids", len(ids))
	}
}
