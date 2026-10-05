// Package service contains the payment service implementation.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
)

// PaymentService реализует сценарии работы с платежами: создание и получение.
// Не знает ни про HTTP, ни про конкретное хранилище - работает через PaymentRepository.
type PaymentService struct {
	payments    PaymentRepository
	idempotency IdempotencyStore
}

// IdempotencyKey - ключ идемпотентности в области одного мерчанта.
type IdempotencyKey struct {
	MerchantID string
	Key        string
}

// CreatePaymentResult - результат создания платежа.
type CreatePaymentResult struct {
	Payment  paymentdomain.Payment
	Replayed bool // true - платёж создан раньше, это повтор
}

// CreatePaymentCommand - входные данные для создания платежа.
// Проверяются в paymentdomain.NewPayment, а не здесь.
type CreatePaymentCommand struct {
	MerchantID     string
	AmountMinor    int64  // сумма в минимальных единицах валюты (копейки, центы)
	Currency       string // код валюты ISO 4217, например "RUB"
	IdempotencyKey string
}

// PaymentRepository - хранилище платежей, которое нужно сервису.
// Если платежа нет, Get возвращает ошибку, оборачивающую paymenterrors.ErrNotFound.
type PaymentRepository interface {
	Save(ctx context.Context, p paymentdomain.Payment) error
	Get(ctx context.Context, id string) (paymentdomain.Payment, error)
}

// IdempotencyStore хранит ключи идемпотентности.
type IdempotencyStore interface {
	// Reserve атомарно занимает ключ. Если операция с этим ключом уже завершена
	// с тем же отпечатком, возвращает id созданного тогда платежа. Если она ещё
	// выполняется - paymenterrors.ErrIdempotencyInProgress, если отпечаток
	// другой - paymenterrors.ErrIdempotencyKeyReused.
	Reserve(ctx context.Context, key IdempotencyKey, fingerprint string) (paymentID string, err error)
	// Complete помечает операцию завершённой и запоминает её результат.
	Complete(ctx context.Context, key IdempotencyKey, paymentID string) error
	// Release освобождает ключ после неудачи, чтобы повтор мог выполниться заново.
	Release(ctx context.Context, key IdempotencyKey) error
}

// NewPaymentService создаёт сервис поверх репозитория платежей и хранилища
// ключей идемпотентности.
func NewPaymentService(payments PaymentRepository, idempotency IdempotencyStore) *PaymentService {
	return &PaymentService{
		payments:    payments,
		idempotency: idempotency,
	}
}

// fingerprint - отпечаток бизнес-содержимого запроса.
func fingerprint(cmd CreatePaymentCommand) string {
	b, _ := json.Marshal(struct {
		MerchantID  string `json:"merchant_id"`
		AmountMinor int64  `json:"amount_minor"`
		Currency    string `json:"currency"`
	}{cmd.MerchantID, cmd.AmountMinor, cmd.Currency})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
