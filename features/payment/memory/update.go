package memory

import (
	"context"
	"fmt"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	"github.com/cagitic1-source/payflow/internal/core/errors/paymenterrors"
)

// Update сохраняет изменения существующего платежа.
func (r *PaymentRepository) Update(_ context.Context, p paymentdomain.Payment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.payments[p.ID]; !ok {
		return fmt.Errorf("payment %s: %w", p.ID, paymenterrors.ErrNotFound)
	}
	r.payments[p.ID] = p
	return nil
}

// UpdateIfStatus сохраняет p, только если в хранилище платёж в статусе from.
// Проверка и запись идут под одной блокировкой, поэтому из нескольких
// одновременных вызовов с одним from успешен ровно один.
func (r *PaymentRepository) UpdateIfStatus(_ context.Context, p paymentdomain.Payment, from paymentdomain.Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.payments[p.ID]
	if !ok {
		return fmt.Errorf("payment %s: %w", p.ID, paymenterrors.ErrNotFound)
	}
	if cur.Status != from {
		return fmt.Errorf("%w: payment %s is %s, want %s", paymenterrors.ErrInvalidTransition, p.ID, cur.Status, from)
	}
	r.payments[p.ID] = p
	return nil
}
