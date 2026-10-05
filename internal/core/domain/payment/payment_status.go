package paymentdomain

import (
	"fmt"
	"time"

	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

// Status - состояние платежа. Платёж создаётся в StatusPending, а дальше
// статус меняется только через методы Payment.
type Status string

// Статусы платежа.
const (
	StatusPending    Status = "pending"    // создан, ждёт обработки
	StatusProcessing Status = "processing" // обрабатывается
	StatusApproved   Status = "approved"   // одобрен; конечный
	StatusDeclined   Status = "declined"   // отказ банка; конечный
	StatusFailed     Status = "failed"     // техническая ошибка; конечный
)

// allowedTransitions - разрешённые переходы. Конечных статусов здесь нет:
// из них перейти никуда нельзя.
var allowedTransitions = map[Status]map[Status]bool{
	StatusPending:    {StatusProcessing: true},
	StatusProcessing: {StatusApproved: true, StatusDeclined: true, StatusFailed: true},
}

// transitionTo меняет статус, если переход разрешён. При ошибке платёж не меняется.
func (p *Payment) transitionTo(to Status, now time.Time) error {
	if !allowedTransitions[p.Status][to] {
		return fmt.Errorf("%w: %s -> %s", paymenterrors.ErrInvalidTransition, p.Status, to)
	}
	p.Status = to
	p.UpdatedAt = now.UTC()
	return nil
}

// StartProcessing переводит платёж в обработку.
func (p *Payment) StartProcessing(now time.Time) error {
	return p.transitionTo(StatusProcessing, now)
}

// Approve отмечает платёж одобренным.
func (p *Payment) Approve(now time.Time) error {
	return p.transitionTo(StatusApproved, now)
}

// Decline отмечает отказ банка с причиной.
func (p *Payment) Decline(now time.Time, reason string) error {
	return p.finishWithReason(StatusDeclined, now, reason)
}

// Fail отмечает техническую ошибку обработки с причиной.
func (p *Payment) Fail(now time.Time, reason string) error {
	return p.finishWithReason(StatusFailed, now, reason)
}

// finishWithReason переводит платёж в declined или failed и запоминает причину.
// Причина проверяется до перехода, чтобы при ошибке платёж не менялся.
func (p *Payment) finishWithReason(to Status, now time.Time, reason string) error {
	if reason == "" {
		return paymenterrors.ErrEmptyFailureReason
	}
	if err := p.transitionTo(to, now); err != nil {
		return err
	}
	p.FailureReason = reason
	return nil
}
