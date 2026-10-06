package service_test

import (
	"context"
	"fmt"
	"sync"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

// Фейки обязаны реализовывать интерфейсы сервиса: при смене сигнатуры
// ошибка компиляции укажет сюда, а не на место вызова.
var (
	_ service.PaymentRepository = (*fakeRepo)(nil)
	_ service.IdempotencyStore  = (*fakeIdempotencyStore)(nil)
	_ service.PaymentQueue      = (*fakeQueue)(nil)
)

type fakeRepo struct {
	saved    []paymentdomain.Payment
	saveErr  error // если задан, Save вернёт его
	getErr   error // если задан, Get вернёт его
	getCalls int
}

func (r *fakeRepo) Save(_ context.Context, p paymentdomain.Payment) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = append(r.saved, p)
	return nil
}

func (r *fakeRepo) Get(_ context.Context, id string) (paymentdomain.Payment, error) {
	r.getCalls++
	if r.getErr != nil {
		return paymentdomain.Payment{}, r.getErr
	}

	for _, p := range r.saved {
		if p.ID == id {
			return p, nil
		}
	}
	return paymentdomain.Payment{}, paymenterrors.ErrNotFound
}

func (r *fakeRepo) Update(_ context.Context, p paymentdomain.Payment) error {
	for i := range r.saved {
		if r.saved[i].ID == p.ID {
			r.saved[i] = p
			return nil
		}
	}
	return fmt.Errorf("payment %s: %w", p.ID, paymenterrors.ErrNotFound)
}

func (r *fakeRepo) UpdateIfStatus(_ context.Context, p paymentdomain.Payment, from paymentdomain.Status) error {
	for i := range r.saved {
		if r.saved[i].ID == p.ID {
			if r.saved[i].Status != from {
				return fmt.Errorf("%w: payment %s is %s, want %s", paymenterrors.ErrInvalidTransition, p.ID, r.saved[i].Status, from)
			}
			r.saved[i] = p
			return nil
		}
	}
	return fmt.Errorf("payment %s: %w", p.ID, paymenterrors.ErrNotFound)
}

type fakeIdempotencyEntry struct {
	fingerprint string
	paymentID   string // пусто, пока операция выполняется
}

// fakeIdempotencyStore повторяет контракт service.IdempotencyStore без TTL.
// Нулевое значение готово к работе.
type fakeIdempotencyStore struct {
	mu           sync.RWMutex
	entries      map[service.IdempotencyKey]fakeIdempotencyEntry
	reserveErr   error // если задан, Reserve вернёт его
	completeErr  error // если задан, Complete вернёт его
	releaseCalls int
}

func (s *fakeIdempotencyStore) Reserve(_ context.Context, key service.IdempotencyKey, fingerprint string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.reserveErr != nil {
		return "", s.reserveErr
	}
	if s.entries == nil {
		s.entries = make(map[service.IdempotencyKey]fakeIdempotencyEntry)
	}

	e, ok := s.entries[key]
	switch {
	case !ok:
		s.entries[key] = fakeIdempotencyEntry{fingerprint: fingerprint}
		return "", nil
	case e.fingerprint != fingerprint:
		return "", paymenterrors.ErrIdempotencyKeyReused
	case e.paymentID == "":
		return "", paymenterrors.ErrIdempotencyInProgress
	default:
		return e.paymentID, nil
	}
}

func (s *fakeIdempotencyStore) Complete(_ context.Context, key service.IdempotencyKey, paymentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.completeErr != nil {
		return s.completeErr
	}

	e, ok := s.entries[key]
	if !ok {
		return fmt.Errorf("idempotency key %s not reserved", key.Key)
	}
	e.paymentID = paymentID
	s.entries[key] = e
	return nil
}

func (s *fakeIdempotencyStore) Release(_ context.Context, key service.IdempotencyKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.releaseCalls++
	if e, ok := s.entries[key]; ok && e.paymentID == "" {
		delete(s.entries, key)
	}
	return nil
}

// fakeQueue повторяет контракт service.PaymentQueue и запоминает вызовы.
// Нулевое значение готово к работе и принимает любой платёж.
type fakeQueue struct {
	mu           sync.Mutex
	full         bool     // если true, TryAcquire отказывает
	acquired     int      // сколько мест занято и ещё не возвращено
	enqueued     []string // id платежей в порядке постановки в очередь
	releaseCalls int
}

func (q *fakeQueue) TryAcquire() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.full {
		return false
	}
	q.acquired++
	return true
}

func (q *fakeQueue) Release() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.releaseCalls++
	q.acquired--
}

func (q *fakeQueue) Enqueue(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.enqueued = append(q.enqueued, id)
}

func validPaymentCommand() service.CreatePaymentCommand {
	return service.CreatePaymentCommand{
		MerchantID:     "merchant1",
		AmountMinor:    1000,
		Currency:       "USD",
		IdempotencyKey: "key-1",
	}
}
