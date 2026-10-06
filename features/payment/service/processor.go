package service

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

// Причины неудачной обработки, которые выставляет сам payflow.
// Причины отказа банка приходят от эквайера.
const (
	ReasonAcquirerTimeout       = "acquirer_timeout"
	ReasonAcquirerUnavailable   = "acquirer_unavailable"
	ReasonProcessingInterrupted = "processing_interrupted"
	ReasonUnknownDecline        = "unknown_decline" // банк отказал, не указав причину
)

// Processor проводит платёж через эквайера. Вызывается воркером пула.
type Processor struct {
	payments PaymentRepository
	acquirer Acquirer
	timeout  time.Duration // сколько ждать ответа эквайера
	log      *zap.Logger
	now      func() time.Time
}

// NewProcessor возвращает обработчик платежей.
func NewProcessor(
	payments PaymentRepository,
	acquirer Acquirer,
	timeout time.Duration,
	log *zap.Logger) *Processor {
	return &Processor{
		payments: payments,
		acquirer: acquirer,
		timeout:  timeout,
		log:      log,
		now:      time.Now}
}

// Process переводит платёж pending → processing → итоговый статус.
// ctx - контекст пула: он отменяется только при принудительной остановке.
func (pr *Processor) Process(ctx context.Context, id string) {
	log := pr.log.With(zap.String("payment_id", id))

	// Пул останавливают принудительно: платёж не трогаем, он остаётся pending.
	if ctx.Err() != nil {
		log.Warn("processing skipped: shutting down")
		return
	}

	p, err := pr.payments.Get(ctx, id)
	if err != nil {
		log.Error("get payment", zap.Error(err))
		return
	}

	if err := p.StartProcessing(pr.now()); err != nil {
		// Платёж уже не pending, например его доставили повторно.
		log.Warn("processing skipped", zap.Error(err))
		return
	}
	// Защита от двойного списания. Проверка выше смотрит на копию платежа,
	// а её мог одновременно прочитать другой воркер. Сохраняем processing,
	// только если в хранилище платёж всё ещё pending: из двух воркеров это
	// удастся одному, и в банк уйдёт один запрос.
	if err := pr.payments.UpdateIfStatus(ctx, p, paymentdomain.StatusPending); err != nil {
		if errors.Is(err, paymenterrors.ErrInvalidTransition) {
			log.Warn("processing skipped: taken by another worker", zap.Error(err))
			return
		}
		log.Error("mark processing", zap.Error(err))
		return
	}

	authCtx, cancel := context.WithTimeout(ctx, pr.timeout)
	decision, err := pr.acquirer.Authorize(authCtx, p)
	cancel()

	now := pr.now()
	switch {
	case err != nil && ctx.Err() != nil:
		// Банк не ответил, потому что пул остановили. Если банк успел
		// ответить (err == nil), его ответ сохраняем ниже, даже при остановке.
		err = p.Fail(now, ReasonProcessingInterrupted)
	case errors.Is(err, context.DeadlineExceeded):
		err = p.Fail(now, ReasonAcquirerTimeout)
	case err != nil:
		err = p.Fail(now, ReasonAcquirerUnavailable)
	case decision.Approved:
		err = p.Approve(now)
	default:
		reason := decision.Reason
		if reason == "" {
			// Эквайер отказал без причины. Decline без причины не сработает,
			// и платёж навсегда останется в processing.
			log.Warn("acquirer declined without reason")
			reason = ReasonUnknownDecline
		}
		err = p.Decline(now, reason)
	}
	if err != nil {
		log.Error("final transition", zap.Error(err))
		return
	}

	// Итог сохраняем даже при остановке: результат банка терять нельзя.
	if err := pr.payments.Update(context.WithoutCancel(ctx), p); err != nil {
		log.Error("save final status", zap.Error(err))
		return
	}
	log.Info("payment processed", zap.String("status", string(p.Status)), zap.String("reason", p.FailureReason))
}
