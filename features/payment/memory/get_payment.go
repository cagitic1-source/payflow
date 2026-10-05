// Package memory contains the in-memory implementation of the payment repository.
package memory

import (
	"context"
	"fmt"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

// Get возвращает платёж по id. Если его нет - ошибку, оборачивающую
// paymenterrors.ErrNotFound.
func (r *PaymentRepository) Get(_ context.Context, id string) (paymentdomain.Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.payments[id]
	if !ok {
		return paymentdomain.Payment{}, fmt.Errorf("payment %s: %w", id, paymenterrors.ErrNotFound)
	}
	return p, nil
}
