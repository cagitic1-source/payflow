// Package service contains the payment service implementation.
package service

import (
	"context"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
)

// PaymentService реализует сценарии работы с платежами: создание и получение.
// Не знает ни про HTTP, ни про конкретное хранилище — работает через PaymentRepository.
type PaymentService struct {
	payments PaymentRepository
}

// CreatePaymentCommand — входные данные для создания платежа.
// Проверяются в paymentdomain.NewPayment, а не здесь.
type CreatePaymentCommand struct {
	MerchantID  string
	AmountMinor int64  // сумма в минимальных единицах валюты (копейки, центы)
	Currency    string // код валюты ISO 4217, например "RUB"
}

// PaymentRepository — хранилище платежей, которое нужно сервису.
// Если платежа нет, Get возвращает ошибку, оборачивающую paymenterrors.ErrNotFound.
type PaymentRepository interface {
	Save(ctx context.Context, p paymentdomain.Payment) error
	Get(ctx context.Context, id string) (paymentdomain.Payment, error)
}

// NewPaymentService создаёт сервис поверх переданного репозитория.
func NewPaymentService(payments PaymentRepository) *PaymentService {
	return &PaymentService{
		payments: payments,
	}
}
