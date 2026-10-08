// Package service contains the payment service implementation.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	"github.com/cagitic1-source/payflow/internal/core/requestctx"
)

// PaymentService реализует сценарии работы с платежами: создание и получение.
// Не знает ни про HTTP, ни про конкретное хранилище - работает через PaymentRepository.
type PaymentService struct {
	payments    PaymentRepository
	idempotency IdempotencyStore
	queue       PaymentQueue
	log         *zap.Logger      // если в ctx нет логгера запроса
	now         func() time.Time // в тестах подменяется
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
	Update(ctx context.Context, p paymentdomain.Payment) error
	// UpdateIfStatus сохраняет p, только если в хранилище платёж сейчас
	// в статусе from. Проверка и запись атомарны. Если статус другой,
	// возвращает ошибку, оборачивающую paymenterrors.ErrInvalidTransition.
	UpdateIfStatus(ctx context.Context, p paymentdomain.Payment, from paymentdomain.Status) error
}

// PaymentQueue - очередь платежей на обработку с ограниченным числом мест.
type PaymentQueue interface {
	TryAcquire() bool // занять место; false - мест нет
	Release()         // вернуть место, если платёж не попал в очередь
	// Enqueue ставит платёж в очередь. Место переходит очереди даже при
	// ошибке, поэтому Release после Enqueue вызывать нельзя.
	Enqueue(id string) error
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

// Decision - ответ эквайера на запрос авторизации.
type Decision struct {
	Approved bool
	Reason   string // причина отказа; пусто, если одобрено
}

// Acquirer авторизует платёж у банка-эквайера.
// Отказ банка - это Decision с Approved == false и причиной.
// Ошибка - банк не ответил: paymenterrors.ErrAcquirerUnavailable или ошибка ctx.
type Acquirer interface {
	Authorize(ctx context.Context, p paymentdomain.Payment) (Decision, error)
}

// NewPaymentService создаёт сервис поверх репозитория платежей и хранилища
// ключей идемпотентности.
func NewPaymentService(
	payments PaymentRepository,
	idempotency IdempotencyStore,
	queue PaymentQueue,
	log *zap.Logger) *PaymentService {
	return &PaymentService{
		payments:    payments,
		idempotency: idempotency,
		queue:       queue,
		log:         log,
		now:         time.Now,
	}
}

// logger возвращает логгер запроса из ctx (с request_id), а если его там
// нет - логгер сервиса.
func (s *PaymentService) logger(ctx context.Context) *zap.Logger {
	if log := requestctx.Logger(ctx); log != nil {
		return log
	}
	return s.log
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
