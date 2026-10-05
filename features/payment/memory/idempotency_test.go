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

const ttl = 24 * time.Hour

// newTestStore возвращает хранилище с управляемыми часами.
// Изменение *clock сдвигает время, которое видит хранилище.
func newTestStore() (*IdempotencyStore, *time.Time) {
	clock := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	s := NewIdempotencyStore(ttl)
	s.now = func() time.Time { return clock }
	return s, &clock
}

var keyA = service.IdempotencyKey{MerchantID: "m-1", Key: "abc"}

// mustReserve занимает ключ и падает, если это не удалось.
func mustReserve(t *testing.T, s *IdempotencyStore, key service.IdempotencyKey, fp string) {
	t.Helper()
	id, err := s.Reserve(t.Context(), key, fp)
	if err != nil {
		t.Fatalf("reserve %v: got (%q, %v), want (\"\", nil)", key, id, err)
	}
}

// assertReserve проверяет результат Reserve.
func assertReserve(t *testing.T, s *IdempotencyStore, key service.IdempotencyKey, fp string, wantID string, wantErr error) {
	t.Helper()
	id, err := s.Reserve(t.Context(), key, fp)
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("reserve: want error %v, got (%q, %v)", wantErr, id, err)
		}
		return
	}
	if err != nil || id != wantID {
		t.Fatalf("reserve: got (%q, %v), want (%q, nil)", id, err, wantID)
	}
}

func TestIdempotencyStore(t *testing.T) {

	t.Run("new key is reserved", func(t *testing.T) {
		s, _ := newTestStore()
		assertReserve(t, s, keyA, "fp1", "", nil)
	})

	t.Run("completed key returns payment id", func(t *testing.T) {
		s, _ := newTestStore()
		mustReserve(t, s, keyA, "fp1")
		if err := s.Complete(t.Context(), keyA, "pay-1"); err != nil {
			t.Fatalf("complete: %v", err)
		}
		assertReserve(t, s, keyA, "fp1", "pay-1", nil)
	})

	t.Run("reserved but not completed is in progress", func(t *testing.T) {
		s, _ := newTestStore()
		mustReserve(t, s, keyA, "fp1")
		assertReserve(t, s, keyA, "fp1", "", paymenterrors.ErrIdempotencyInProgress)
	})

	t.Run("different fingerprint is reuse", func(t *testing.T) {
		s, _ := newTestStore()
		mustReserve(t, s, keyA, "fp1")
		if err := s.Complete(t.Context(), keyA, "pay-1"); err != nil {
			t.Fatalf("complete: %v", err)
		}
		assertReserve(t, s, keyA, "fp2", "", paymenterrors.ErrIdempotencyKeyReused)
	})

	t.Run("release frees key", func(t *testing.T) {
		s, _ := newTestStore()
		mustReserve(t, s, keyA, "fp1")
		_ = s.Release(t.Context(), keyA)
		assertReserve(t, s, keyA, "fp1", "", nil)
	})

	t.Run("release does not free completed key", func(t *testing.T) {
		s, _ := newTestStore()
		mustReserve(t, s, keyA, "fp1")
		_ = s.Complete(t.Context(), keyA, "pay-1")
		_ = s.Release(t.Context(), keyA)
		assertReserve(t, s, keyA, "fp1", "pay-1", nil)
	})

	t.Run("key is alive just before ttl", func(t *testing.T) {
		s, clock := newTestStore()
		mustReserve(t, s, keyA, "fp1")
		_ = s.Complete(t.Context(), keyA, "pay-1")
		*clock = clock.Add(ttl - time.Second)
		assertReserve(t, s, keyA, "fp1", "pay-1", nil)
	})

	t.Run("expired key is free again", func(t *testing.T) {
		s, clock := newTestStore()
		mustReserve(t, s, keyA, "fp1")
		_ = s.Complete(t.Context(), keyA, "pay-1")
		*clock = clock.Add(ttl + time.Second)
		assertReserve(t, s, keyA, "fp1", "", nil)
	})

	t.Run("complete extends ttl", func(t *testing.T) {
		s, clock := newTestStore()
		mustReserve(t, s, keyA, "fp1")
		*clock = clock.Add(20 * time.Hour)
		_ = s.Complete(t.Context(), keyA, "pay-1")
		*clock = clock.Add(10 * time.Hour)
		assertReserve(t, s, keyA, "fp1", "pay-1", nil)
	})

	t.Run("complete without reserve fails", func(t *testing.T) {
		s, _ := newTestStore()
		if err := s.Complete(t.Context(), keyA, "pay-1"); err == nil {
			t.Fatal("complete without reserve: want error, got nil")
		}
	})

	t.Run("same key for different merchants is independent", func(t *testing.T) {
		s, _ := newTestStore()
		mustReserve(t, s, service.IdempotencyKey{MerchantID: "m-1", Key: "abc"}, "fp1")
		mustReserve(t, s, service.IdempotencyKey{MerchantID: "m-2", Key: "abc"}, "fp1")
	})

}

func TestIdempotencyStore_ConcurrentReserve(t *testing.T) {
	s, _ := newTestStore()
	const n = 50

	var winners, inProgress, other atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{}) // стартовый барьер

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			id, err := s.Reserve(t.Context(), keyA, "fp1")
			switch {
			case err == nil && id == "":
				winners.Add(1)
			case errors.Is(err, paymenterrors.ErrIdempotencyInProgress):
				inProgress.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	close(start) // отпускаем всех одновременно
	wg.Wait()

	if winners.Load() != 1 || inProgress.Load() != n-1 || other.Load() != 0 {
		t.Fatalf("winners=%d inProgress=%d other=%d, want 1/%d/0",
			winners.Load(), inProgress.Load(), other.Load(), n-1)
	}

}

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
