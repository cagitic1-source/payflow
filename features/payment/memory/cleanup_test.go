package memory

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cagitic1-source/payflow/features/payment/service"
)

// assertKeys проверяет, какие ключи остались в хранилище (в любом порядке).
func assertKeys(t *testing.T, s *IdempotencyStore, want ...string) {
	t.Helper()
	s.mu.Lock()
	got := make([]string, 0, len(s.entries))
	for k := range s.entries {
		got = append(got, k.Key)
	}
	s.mu.Unlock()

	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("keys %v, want %v", got, want)
	}
}

// DeleteExpired удаляет только истёкшие записи, граница - как в Reserve:
// при now == expiresAt запись жива, через наносекунду - уже нет.
// Если очистка заденет живой ключ, повтор запроса создаст второй платёж.
func TestIdempotencyStore_DeleteExpired(t *testing.T) {
	s, clock := newTestStore()
	start := *clock
	key := func(k string) service.IdempotencyKey {
		return service.IdempotencyKey{MerchantID: "m-1", Key: k}
	}
	complete := func(k string) {
		t.Helper()
		mustReserve(t, s, key(k), "fp")
		if err := s.Complete(t.Context(), key(k), "pay-"+k); err != nil {
			t.Fatalf("complete %s: %v", k, err)
		}
	}

	complete("old")                       // истекает в start+ttl
	mustReserve(t, s, key("stuck"), "fp") // не завершён, истекает в start+ttl
	*clock = start.Add(time.Hour)
	complete("edge") // истекает в start+1h+ttl
	*clock = start.Add(2 * time.Hour)
	complete("fresh") // истекает в start+2h+ttl

	// Ровно на границе edge: удаляются только old и stuck.
	*clock = start.Add(time.Hour + ttl)
	if n := s.DeleteExpired(); n != 2 {
		t.Errorf("deleted %d, want 2", n)
	}
	assertKeys(t, s, "edge", "fresh")
	// Живые ключи по-прежнему защищают от повтора.
	assertReserve(t, s, key("edge"), "fp", "pay-edge", nil)
	assertReserve(t, s, key("fresh"), "fp", "pay-fresh", nil)

	if n := s.DeleteExpired(); n != 0 {
		t.Errorf("second cleanup deleted %d, want 0", n)
	}

	// Наносекунда после границы: edge истёк.
	*clock = clock.Add(time.Nanosecond)
	if n := s.DeleteExpired(); n != 1 {
		t.Errorf("deleted %d after edge expired, want 1", n)
	}
	assertKeys(t, s, "fresh")
}

// RunCleanup раз в interval вызывает очистку и выходит по отмене ctx.
// synctest подменяет часы и тикер: 25 часов проходят мгновенно.
func TestIdempotencyStore_RunCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewIdempotencyStore(ttl) // time.Now внутри пузыря - фейковое время
		mustReserve(t, s, keyA, "fp")
		if err := s.Complete(t.Context(), keyA, "pay-1"); err != nil {
			t.Fatalf("complete: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			s.RunCleanup(ctx, time.Hour)
			close(done)
		}()

		// Тики идут каждый час. Середины интервалов выбраны, чтобы не
		// совпадать с тиком: на тике в ttl ключ ещё жив (граница), на
		// следующем - удаляется.
		time.Sleep(ttl + 30*time.Minute)
		synctest.Wait()
		assertKeys(t, s, keyA.Key)

		time.Sleep(time.Hour)
		synctest.Wait()
		assertKeys(t, s)

		// Пока тикер жив, synctest двигает время бесконечно, поэтому <-done
		// при сломанном RunCleanup висел бы до таймаута go test.
		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("RunCleanup did not stop after ctx cancel")
		}
	})
}
