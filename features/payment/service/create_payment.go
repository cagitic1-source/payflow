package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
)

// CreatePayment создаёт платёж с новым id (UUIDv7) и сохраняет его.
// Ошибки валидации оборачивают paymenterrors.ErrValidation.
func (s *PaymentService) CreatePayment(ctx context.Context, cmd CreatePaymentCommand) (paymentdomain.Payment, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return paymentdomain.Payment{}, fmt.Errorf("generate payment id: %w", err)
	}

	p, err := paymentdomain.NewPayment(
		id.String(),
		cmd.MerchantID,
		cmd.AmountMinor,
		cmd.Currency,
	)
	if err != nil {
		return paymentdomain.Payment{}, err
	}

	if err := s.payments.Save(ctx, p); err != nil {
		return paymentdomain.Payment{}, fmt.Errorf("save payment: %w", err)
	}

	return p, nil
}
