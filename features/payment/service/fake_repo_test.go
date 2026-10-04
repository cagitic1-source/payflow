package service_test

import (
	"context"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

type fakeRepo struct {
	saved    []paymentdomain.Payment
	saveErr  error // если задан, Save вернёт его
	getErr   error // если задан, Get вернёт его
	getCalls int
}

func (r *fakeRepo) Save(_ context.Context, p paymentdomain.Payment) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = append(r.saved, p)
	return nil
}

func (r *fakeRepo) Get(_ context.Context, id string) (paymentdomain.Payment, error) {
	r.getCalls++
	if r.getErr != nil {
		return paymentdomain.Payment{}, r.getErr
	}

	for _, p := range r.saved {
		if p.ID == id {
			return p, nil
		}
	}
	return paymentdomain.Payment{}, paymenterrors.ErrNotFound
}

func validPaymentCommand() service.CreatePaymentCommand {
	return service.CreatePaymentCommand{
		MerchantID:  "merchant1",
		AmountMinor: 1000,
		Currency:    "USD",
	}
}
