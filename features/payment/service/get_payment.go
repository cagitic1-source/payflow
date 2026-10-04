package service

import (
	"context"
	"fmt"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

// GetPayment возвращает платёж по id. Для пустого id возвращает
// paymenterrors.ErrEmptyPaymentID, для несуществующего — ошибку с paymenterrors.ErrNotFound.
func (s *PaymentService) GetPayment(ctx context.Context, id string) (paymentdomain.Payment, error) {
	if id == "" {
		return paymentdomain.Payment{}, paymenterrors.ErrEmptyPaymentID
	}

	payment, err := s.payments.Get(ctx, id)
	if err != nil {
		return paymentdomain.Payment{}, fmt.Errorf("get payment: %w", err)
	}
	return payment, nil

}
