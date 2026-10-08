// Package paymenterrors contains payment domain errors.
package paymenterrors

import (
	"errors"
	"fmt"
)

// Ошибки валидации платежа. Все они оборачивают ErrValidation,
// поэтому errors.Is(err, ErrValidation) отличает их от остальных.
var (
	ErrValidation          = errors.New("validation failed")
	ErrEmptyPaymentID      = fmt.Errorf("%w: payment id is empty", ErrValidation)
	ErrEmptyMerchantID     = fmt.Errorf("%w: merchant id is empty", ErrValidation)
	ErrInvalidAmount       = fmt.Errorf("%w: amount must be positive", ErrValidation)
	ErrEmptyCurrency       = fmt.Errorf("%w: currency is empty", ErrValidation)
	ErrUnsupportedCurrency = fmt.Errorf("%w: currency is not supported", ErrValidation)
)

var (
	// ErrNotFound - платежа с таким id нет.
	ErrNotFound = errors.New("payment not found")
)

var (
	// ErrIdempotencyKeyReused - ключ уже использован для другого запроса.
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with different request")
	// ErrIdempotencyInProgress - запрос с этим ключом ещё выполняется.
	ErrIdempotencyInProgress = errors.New("request with this idempotency key is in progress")
	// ErrEmptyIdempotencyKey - запрос пришёл без ключа идемпотентности.
	ErrEmptyIdempotencyKey = fmt.Errorf("%w: idempotency key is empty", ErrValidation)
)

var (
	// ErrOverloaded - система не может принять платёж прямо сейчас.
	ErrOverloaded = errors.New("service is overloaded")
)

// Ошибки смены статуса платежа. Это не ошибки входных данных,
// поэтому ErrValidation они не оборачивают.
var (
	// ErrInvalidTransition - недопустимая смена статуса платежа.
	ErrInvalidTransition = errors.New("invalid payment status transition")
	// ErrEmptyFailureReason - отказ или ошибка без причины.
	ErrEmptyFailureReason = errors.New("failure reason is empty")
)

var (
	// ErrAcquirerUnavailable - эквайер не ответил из-за технического сбоя.
	ErrAcquirerUnavailable = errors.New("acquirer unavailable")
)
