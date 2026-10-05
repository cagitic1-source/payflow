package router_test

import (
	"context"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
)

type fakePaymentService struct {
	payment     paymentdomain.Payment
	replayed    bool // Replayed в ответе CreatePayment
	err         error
	createCalls int
	lastCmd     service.CreatePaymentCommand // команда последнего вызова CreatePayment
}

func (s *fakePaymentService) CreatePayment(_ context.Context, cmd service.CreatePaymentCommand) (service.CreatePaymentResult, error) {
	s.createCalls++
	s.lastCmd = cmd
	return service.CreatePaymentResult{Payment: s.payment, Replayed: s.replayed}, s.err
}

func (s *fakePaymentService) GetPayment(_ context.Context, _ string) (paymentdomain.Payment, error) {
	return s.payment, s.err
}
