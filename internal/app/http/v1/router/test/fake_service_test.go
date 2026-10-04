package router_test

import (
	"context"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
)

type fakePaymentService struct {
	payment     paymentdomain.Payment
	err         error
	createCalls int
}

func (s *fakePaymentService) CreatePayment(_ context.Context, _ service.CreatePaymentCommand) (paymentdomain.Payment, error) {
	s.createCalls++
	return s.payment, s.err
}

func (s *fakePaymentService) GetPayment(_ context.Context, _ string) (paymentdomain.Payment, error) {
	return s.payment, s.err
}
