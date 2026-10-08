package httptransport

import (
	"context"
	"errors"
	"net/http"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	"github.com/cagitic1-source/payflow/internal/core/requestctx"
)

const idempotencyKeyHeader = "Idempotency-Key"

// maxRequestBodyBytes - предел размера тела запроса. Тело больше - 400 malformed-request.
const maxRequestBodyBytes = 1 << 20

// ErrMalformedRequest - тело запроса не удалось разобрать: невалидный JSON,
// неизвестные поля или лишние данные после объекта. Клиент получает 400.
var ErrMalformedRequest = errors.New("malformed request")

// IdempotentReplayedHeader ставится в ответ, если платёж создан раньше
// и ответ - повтор первого.
const IdempotentReplayedHeader = "Idempotent-Replayed"

// PaymentHandler - HTTP-обработчики платёжного API.
type PaymentHandler struct {
	svc PaymentService
	log *zap.Logger
}

// PaymentService - то, что обработчикам нужно от сервисного слоя.
// Его ошибки превращаются в HTTP-ответы по таблице problemSpecs.
type PaymentService interface {
	CreatePayment(ctx context.Context, cmd service.CreatePaymentCommand) (service.CreatePaymentResult, error)
	GetPayment(ctx context.Context, id string) (paymentdomain.Payment, error)
}

// NewPaymentHandler создаёт обработчики поверх сервиса платежей.
func NewPaymentHandler(paymentService PaymentService, log *zap.Logger) *PaymentHandler {
	return &PaymentHandler{
		svc: paymentService,
		log: log,
	}
}

// requestLogger возвращает логгер запроса с полем request_id, который кладёт
// middleware.RequestID. Если его нет (например, в тестах без middleware),
// возвращает fallback.
func requestLogger(r *http.Request, fallback *zap.Logger) *zap.Logger {
	if log := requestctx.Logger(r.Context()); log != nil {
		return log
	}
	return fallback
}
