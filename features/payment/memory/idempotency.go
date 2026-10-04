package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

type idempotencyEntry struct {
	fingerprint string
	paymentID   string // пусто, пока операция выполняется
	expiresAt   time.Time
}

// IdempotencyStore хранит ключи идемпотентности в памяти процесса.
type IdempotencyStore struct {
	mu      sync.Mutex
	entries map[service.IdempotencyKey]idempotencyEntry
	ttl     time.Duration
	now     func() time.Time // в тестах подменяется
}

// NewIdempotencyStore возвращает хранилище, где ключи живут ttl.
func NewIdempotencyStore(ttl time.Duration) *IdempotencyStore {
	return &IdempotencyStore{
		entries: make(map[service.IdempotencyKey]idempotencyEntry),
		ttl:     ttl,
		now:     time.Now,
	}
}

func (s *IdempotencyStore) Reserve(_ context.Context, key service.IdempotencyKey, fp string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	e, ok := s.entries[key]

	// Истёкшая запись — как будто её нет.
	if ok && now.After(e.expiresAt) {
		ok = false
	}

	// Ключа нет: занимаем его. paymentID пустой — значит «выполняется».
	if !ok {
		s.entries[key] = idempotencyEntry{
			fingerprint: fp,
			expiresAt:   now.Add(s.ttl),
		}
		return "", nil
	}
	// Ключ есть, но запрос другой — клиент перепутал ключи.
	if e.fingerprint != fp {
		return "", paymenterrors.ErrIdempotencyKeyReused
	}

	// Тот же запрос, но первый ещё выполняется — второй создавать нельзя.
	if e.paymentID == "" {
		return "", paymenterrors.ErrIdempotencyInProgress
	}

	// Тот же запрос, первый уже завершён — повтор.
	return e.paymentID, nil
}

// Complete помечает операцию завершённой и запоминает её результат.
func (s *IdempotencyStore) Complete(_ context.Context, key service.IdempotencyKey, paymentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]
	if !ok {
		return fmt.Errorf("idempotency key %s not reserved", key.Key)
	}

	e.expiresAt = s.now().Add(s.ttl)
	e.paymentID = paymentID
	s.entries[key] = e

	return nil
}

// Release освобождает ключ после неудачи, чтобы повтор мог выполниться заново.
func (s *IdempotencyStore) Release(ctx context.Context, key service.IdempotencyKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.entries[key]; ok && e.paymentID != "" {
		return nil
	}
	delete(s.entries, key)
	return nil
}
