// Package paymenterrors contains payment domain errors.
package paymenterrors

import (
	"errors"
	"fmt"
)

// Ошибки платёжного домена. Все ошибки валидации оборачивают ErrValidation,
// поэтому errors.Is(err, ErrValidation) отличает их от остальных.
var (
	ErrValidation          = errors.New("validation failed")
	ErrNotFound            = errors.New("payment not found")
	ErrEmptyPaymentID      = fmt.Errorf("%w: payment id is empty", ErrValidation)
	ErrEmptyMerchantID     = fmt.Errorf("%w: merchant id is empty", ErrValidation)
	ErrInvalidAmount       = fmt.Errorf("%w: amount must be positive", ErrValidation)
	ErrEmptyCurrency       = fmt.Errorf("%w: currency is empty", ErrValidation)
	ErrUnsupportedCurrency = fmt.Errorf("%w: currency is not supported", ErrValidation)
)

var (
	// ErrIdempotencyKeyReused — ключ уже использован для другого запроса.
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with different request")
	// ErrIdempotencyInProgress — запрос с этим ключом ещё выполняется.
	ErrIdempotencyInProgress = errors.New("request with this idempotency key is in progress")
	// ErrEmptyIdempotencyKey — запрос пришёл без ключа идемпотентности.
	ErrEmptyIdempotencyKey = fmt.Errorf("%w: idempotency key is empty", ErrValidation)
)
