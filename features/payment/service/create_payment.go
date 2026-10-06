package service

import (
	"context"
	"fmt"
	"uuid"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

// CreatePayment создаёт платёж ровно один раз для каждого ключа идемпотентности.
// Повтор с тем же ключом и тем же содержимым возвращает ранее созданный платёж.
func (s *PaymentService) CreatePayment(ctx context.Context, cmd CreatePaymentCommand) (CreatePaymentResult, error) {
	if cmd.IdempotencyKey == "" {
		return CreatePaymentResult{}, paymenterrors.ErrEmptyIdempotencyKey
	}

	key := IdempotencyKey{
		MerchantID: cmd.MerchantID,
		Key:        cmd.IdempotencyKey,
	}

	id := uuid.NewV7()
	p, err := paymentdomain.NewPayment(id.String(), cmd.MerchantID, cmd.AmountMinor, cmd.Currency, s.now())
	if err != nil {
		return CreatePaymentResult{}, err
	}

	existingID, err := s.idempotency.Reserve(ctx, key, fingerprint(cmd))
	if err != nil {
		// Конфликт или повторное использование ключа - создавать нельзя.
		return CreatePaymentResult{}, fmt.Errorf("reserve idempotency key: %w", err)
	}
	if existingID != "" {
		existing, err := s.payments.Get(ctx, existingID)
		if err != nil {
			return CreatePaymentResult{}, fmt.Errorf("get replayed payment: %w", err)
		}
		return CreatePaymentResult{Payment: existing, Replayed: true}, nil
	}

	saved := false
	defer func() {
		if !saved {
			_ = s.idempotency.Release(context.WithoutCancel(ctx), key)
		}
	}()

	// Место в очереди занимаем до сохранения: если мест нет, ничего не сохраняем.
	if !s.queue.TryAcquire() {
		return CreatePaymentResult{}, paymenterrors.ErrOverloaded
	}

	enqueued := false
	defer func() {
		if !enqueued {
			s.queue.Release()
		}
	}()

	if err := s.payments.Save(ctx, p); err != nil {
		return CreatePaymentResult{}, fmt.Errorf("save payment: %w", err)
	}
	saved = true

	s.queue.Enqueue(p.ID)
	enqueued = true

	if err := s.idempotency.Complete(ctx, key, p.ID); err != nil {
		return CreatePaymentResult{}, fmt.Errorf("complete payment: %w", err)
	}
	return CreatePaymentResult{
		Payment: p,
	}, nil
}
