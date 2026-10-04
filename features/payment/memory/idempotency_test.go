package memory

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

func TestIdempotencyStore_Expired(t *testing.T) {
	const ttl = 24 * time.Hour
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	store := NewIdempotencyStore(ttl)
	store.now = func() time.Time { return now }

	key := service.IdempotencyKey{MerchantID: "m-1", Key: "abc"}
	const fp = "Алабай"

	// Первый запрос: ключа нет — резервируем.
	got, err := store.Reserve(t.Context(), key, fp)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if got != "" {
		t.Fatalf("Reserve нового ключа вернул %q, ожидался пустой id", got)
	}
	if err := store.Complete(t.Context(), key, "pay-1"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// Ровно на границе TTL запись ещё жива — повтор возвращает тот же платёж.
	// Без этой проверки тест прошёл бы, даже если запись просто пропала.
	now = now.Add(ttl)
	got, err = store.Reserve(t.Context(), key, fp)
	if err != nil {
		t.Fatalf("Reserve на границе TTL: %v", err)
	}
	if got != "pay-1" {
		t.Fatalf("Reserve на границе TTL вернул %q, ожидался %q", got, "pay-1")
	}

	// Прошло 25 часов: истёкшая запись считается отсутствующей.
	now = now.Add(time.Hour)
	got, err = store.Reserve(t.Context(), key, fp)
	if err != nil {
		t.Fatalf("Reserve после истечения: %v", err)
	}
	if got != "" {
		t.Fatalf("Reserve после истечения вернул %q, ожидался пустой id", got)
	}
}

func TestIdempotencyStore_InProgress(t *testing.T) {
	store := NewIdempotencyStore(24 * time.Hour)
	key := service.IdempotencyKey{MerchantID: "m-1", Key: "abc"}
	const fp = "Алабай"

	if _, err := store.Reserve(t.Context(), key, fp); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	// Первый ещё не завершён — повтор должен получить отказ, а не «создавай».
	got, err := store.Reserve(t.Context(), key, fp)
	if !errors.Is(err, paymenterrors.ErrIdempotencyInProgress) {
		t.Fatalf("Reserve во время выполнения: ошибка %v, ожидалась %v", err, paymenterrors.ErrIdempotencyInProgress)
	}
	if got != "" {
		t.Fatalf("Reserve во время выполнения вернул %q, ожидался пустой id", got)
	}

	// Первый упал и освободил ключ — повтор может выполниться заново.
	if err := store.Release(t.Context(), key); err != nil {
		t.Fatalf("Release: %v", err)
	}
	got, err = store.Reserve(t.Context(), key, fp)
	if err != nil {
		t.Fatalf("Reserve после Release: %v", err)
	}
	if got != "" {
		t.Fatalf("Reserve после Release вернул %q, ожидался пустой id", got)
	}
}

func TestIdempotencyStore_ConcurrentReserve(t *testing.T) {
	store := NewIdempotencyStore(24 * time.Hour)
	key := service.IdempotencyKey{MerchantID: "m-1", Key: "abc"}
	const fp = "Алабай"
	const n = 100

	var reserved, inProgress atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			<-start // все горутины стартуют одновременно
			got, err := store.Reserve(t.Context(), key, fp)
			switch {
			case err == nil && got == "":
				reserved.Add(1)
			case errors.Is(err, paymenterrors.ErrIdempotencyInProgress):
				inProgress.Add(1)
			default:
				t.Errorf("Reserve: неожиданный результат %q, %v", got, err)
			}
		})
	}
	close(start)
	wg.Wait()

	// Ровно один запрос получает право создать платёж, остальные — отказ.
	if got := reserved.Load(); got != 1 {
		t.Fatalf("ключ зарезервировали %d раз, ожидался 1", got)
	}
	if got := inProgress.Load(); got != n-1 {
		t.Fatalf("ErrIdempotencyInProgress получили %d, ожидалось %d", got, n-1)
	}

	// После завершения повторы получают уже созданный платёж.
	if err := store.Complete(t.Context(), key, "pay-1"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got, err := store.Reserve(t.Context(), key, fp)
	if err != nil {
		t.Fatalf("Reserve после Complete: %v", err)
	}
	if got != "pay-1" {
		t.Fatalf("Reserve после Complete вернул %q, ожидался %q", got, "pay-1")
	}
}
