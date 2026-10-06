// Package acquirer содержит фейковый банк-эквайер для разработки и тестов.
package acquirer

import (
	"context"
	"math/rand"
	"time"

	paymentservice "github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

// Проверка на этапе компиляции: Fake реализует paymentservice.Acquirer.
var _ paymentservice.Acquirer = (*Fake)(nil)

// declineReasons - коды отказа, которые возвращает фейковый банк.
var declineReasons = []string{"insufficient_funds", "card_expired", "suspected_fraud"}

// FakeConfig задаёт поведение фейкового эквайера.
type FakeConfig struct {
	MinLatency  time.Duration // минимальная задержка ответа
	MaxLatency  time.Duration // максимальная задержка ответа
	DeclineRate float64       // доля отказов банка, 0..1
	ErrorRate   float64       // доля технических сбоев, 0..1
}

// DefaultFakeConfig - поведение по умолчанию: ответ за 50-300 мс,
// 5% отказов, 2% сбоев.
func DefaultFakeConfig() FakeConfig {
	return FakeConfig{
		MinLatency:  50 * time.Millisecond,
		MaxLatency:  300 * time.Millisecond,
		DeclineRate: 0.05,
		ErrorRate:   0.02,
	}
}

// Fake имитирует банк-эквайер: случайная задержка и случайный исход.
// Безопасен для конкурентного использования.
type Fake struct {
	cfg FakeConfig

	// outcome выбирает исход и причину отказа: число в [0, 1).
	// В тестах подменяется, чтобы исход был детерминированным.
	outcome func() float64
}

// NewFake возвращает фейковый эквайер.
func NewFake(cfg FakeConfig) *Fake {
	return &Fake{cfg: cfg, outcome: rand.Float64}
}

// Authorize ждёт случайную задержку и возвращает случайный исход.
// Если ctx завершится раньше, возвращает ошибку ctx.
func (f *Fake) Authorize(ctx context.Context, _ paymentdomain.Payment) (paymentservice.Decision, error) {
	timer := time.NewTimer(f.latency())
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return paymentservice.Decision{}, ctx.Err()
	case <-timer.C:
	}

	r := f.outcome()
	switch {
	case r < f.cfg.ErrorRate:
		return paymentservice.Decision{}, paymenterrors.ErrUnavailable
	case r < f.cfg.ErrorRate+f.cfg.DeclineRate:
		return paymentservice.Decision{Reason: declineReasons[int(f.outcome()*float64(len(declineReasons)))]}, nil
	default:
		return paymentservice.Decision{Approved: true}, nil
	}
}

func (f *Fake) latency() time.Duration {
	spread := f.cfg.MaxLatency - f.cfg.MinLatency
	if spread <= 0 {
		return f.cfg.MinLatency
	}
	//nolint:gosec // G404: случайность для имитации задержки, не для безопасности
	return f.cfg.MinLatency + time.Duration(rand.Float64()*float64(spread))
}
