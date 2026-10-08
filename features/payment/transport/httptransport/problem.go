package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"

	"go.uber.org/zap"

	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
	"github.com/cagitic1-source/payflow/internal/core/requestctx"
)

// problem - тело ответа об ошибке по RFC 9457.
type problem struct {
	Type       string `json:"type"`
	Title      string `json:"title"`
	Status     int    `json:"status"`
	Detail     string `json:"detail"`
	Instance   string `json:"instance,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Field      string `json:"field,omitempty"`
	retryAfter string
}

// problemSpec - одна строка таблицы «ошибка → ответ клиенту».
type problemSpec struct {
	err        error
	status     int
	slug       string
	title      string
	detail     string
	field      string
	retryAfter string // если не пусто - заголовок Retry-After
}

const problemTypeBase = "https://payflow.dev/problems/"

var problemSpecs = []problemSpec{
	// --- 422: конкретные ошибки валидации ---
	{
		err: paymenterrors.ErrEmptyMerchantID, status: http.StatusUnprocessableEntity,
		slug: "empty-merchant-id", title: "Empty merchant id",
		detail: "merchant_id must be provided", field: "merchant_id",
	},
	{
		err: paymenterrors.ErrInvalidAmount, status: http.StatusUnprocessableEntity,
		slug: "invalid-amount", title: "Invalid amount",
		detail: "amount_minor must be positive", field: "amount_minor",
	},
	{
		err: paymenterrors.ErrEmptyCurrency, status: http.StatusUnprocessableEntity,
		slug: "empty-currency", title: "Empty currency",
		detail: "currency must be provided", field: "currency",
	},
	{
		err: paymenterrors.ErrUnsupportedCurrency, status: http.StatusUnprocessableEntity,
		slug: "unsupported-currency", title: "Unsupported currency",
		detail: "currency must be one of: RUB, USD, EUR", field: "currency",
	},
	{
		err: paymenterrors.ErrEmptyPaymentID, status: http.StatusUnprocessableEntity,
		slug: "empty-payment-id", title: "Empty payment id",
		detail: "payment id must be provided",
	},
	{
		err: paymenterrors.ErrIdempotencyKeyReused, status: http.StatusUnprocessableEntity,
		slug: "idempotency-key-reused", title: "Idempotency key reused",
		detail: "Idempotency-Key was already used with a different request body",
	},

	// --- 503 ---
	{
		err: paymenterrors.ErrOverloaded, status: http.StatusServiceUnavailable,
		slug: "service-overloaded", title: "Service overloaded",
		detail:     "the service cannot accept payments right now, retry later",
		retryAfter: "1",
	},

	// --- 409 ---
	{
		err: paymenterrors.ErrIdempotencyInProgress, status: http.StatusConflict,
		slug: "idempotency-request-in-progress", title: "Request in progress",
		detail: "a request with this Idempotency-Key is still being processed, retry later",
	},

	// --- 404 ---
	{
		err: paymenterrors.ErrNotFound, status: http.StatusNotFound,
		slug: "payment-not-found", title: "Payment not found",
		detail: "payment with the given id does not exist",
	},
	// --- 400 ---
	{
		err: ErrMalformedRequest, status: http.StatusBadRequest,
		slug: "malformed-request", title: "Malformed request",
		detail: "request body is not valid JSON or contains unknown fields",
	},
	// ErrEmptyIdempotencyKey оборачивает ErrValidation, поэтому стоит выше страховки.
	{
		err: paymenterrors.ErrEmptyIdempotencyKey, status: http.StatusBadRequest,
		slug: "missing-idempotency-key", title: "Missing idempotency key",
		detail: "Idempotency-Key header must be provided",
	},

	// --- 422: страховка для ошибок валидации без своей строки. ВСЕГДА ПОСЛЕДНЯЯ ---
	{
		err: paymenterrors.ErrValidation, status: http.StatusUnprocessableEntity,
		slug: "validation-error", title: "Validation error",
		detail: "request validation failed",
	},
}

func problemFromError(err error) problem {
	for _, spec := range problemSpecs {
		if errors.Is(err, spec.err) {
			return problem{
				Type:       problemTypeBase + spec.slug,
				Title:      spec.title,
				Status:     spec.status,
				Detail:     spec.detail,
				Field:      spec.field,
				retryAfter: spec.retryAfter,
			}
		}
	}

	return problem{
		Type:   "about:blank",
		Title:  http.StatusText(http.StatusInternalServerError),
		Status: http.StatusInternalServerError,
		Detail: "internal error",
	}
}

func respondError(w http.ResponseWriter, r *http.Request, log *zap.Logger, err error) {
	p := problemFromError(err)
	p.Instance = r.URL.Path

	log = requestLogger(r, log)
	fields := []zap.Field{
		zap.String("method", r.Method),
		zap.String("url", r.URL.String()),
		zap.Int("status", p.Status),
		zap.Error(err),
	}
	switch {
	case p.Status == http.StatusServiceUnavailable:
		// 503 - штатный отказ при перегрузке, а не сбой сервиса.
		log.Warn("request rejected", fields...)
	case p.Status >= http.StatusInternalServerError:
		log.Error("request failed", fields...)
	default:
		// 4xx - ошибка клиента, не наша: Info, чтобы не будить дежурных.
		log.Info("request rejected", fields...)
	}

	if p.retryAfter != "" {
		w.Header().Set("Retry-After", p.retryAfter)
	}

	p.RequestID = requestctx.RequestID(r.Context())
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)

	if encErr := json.NewEncoder(w).Encode(p); encErr != nil {
		log.Debug("write problem response", zap.Error(encErr))
	}
}
