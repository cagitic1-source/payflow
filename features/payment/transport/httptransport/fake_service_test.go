package httptransport_test

import (
	"context"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
)

type fakePaymentService struct {
	payment     paymentdomain.Payment
	replayed    bool
	err         error
	createCalls int
	lastCmd     service.CreatePaymentCommand
}

func (f *fakePaymentService) CreatePayment(_ context.Context, cmd service.CreatePaymentCommand) (service.CreatePaymentResult, error) {
	f.createCalls++
	f.lastCmd = cmd
	if f.err != nil {
		return service.CreatePaymentResult{}, f.err
	}
	return service.CreatePaymentResult{Payment: f.payment, Replayed: f.replayed}, nil
}

func (f *fakePaymentService) GetPayment(_ context.Context, _ string) (paymentdomain.Payment, error) {
	return f.payment, f.err
}

// acceptQueue - очередь, которая принимает любой платёж и никуда его не передаёт.
// Этим тестам важен HTTP, а не обработка платежей.
type acceptQueue struct{}

func (acceptQueue) TryAcquire() bool { return true }
func (acceptQueue) Release()         {}
func (acceptQueue) Enqueue(string)   {}
