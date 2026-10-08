package service

import (
	"context"
	"fmt"
	"uuid"

	"go.uber.org/zap"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	"github.com/cagitic1-source/payflow/internal/core/errors/paymenterrors"
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

	// Место переходит очереди и при ошибке: его вернёт воркер или сам Enqueue.
	// Повторный Release освободил бы чужое место.
	enqueued = true
	if err := s.queue.Enqueue(p.ID); err != nil {
		// Сервис останавливается. Платёж уже сохранён в pending и считается
		// принятым: после перехода на PostgreSQL его подберёт восстановление.
		s.logger(ctx).Error("payment saved but not enqueued",
			zap.String("payment_id", p.ID), zap.Error(err))
	}

	if err := s.idempotency.Complete(ctx, key, p.ID); err != nil {
		return CreatePaymentResult{}, fmt.Errorf("complete payment: %w", err)
	}
	return CreatePaymentResult{
		Payment: p,
	}, nil
}
