package memory

import (
	"context"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
)

// Save сохраняет платёж. Платёж с тем же ID перезаписывается.
func (r *PaymentRepository) Save(_ context.Context, p paymentdomain.Payment) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.payments[p.ID] = p
	return nil
}
