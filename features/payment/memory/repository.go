package memory

import (
	"sync"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
)

// PaymentRepository хранит платежи в памяти процесса.
// Безопасен для конкурентного использования.
type PaymentRepository struct {
	mu       sync.RWMutex
	payments map[string]paymentdomain.Payment
}

// NewPaymentRepository возвращает пустое хранилище.
func NewPaymentRepository() *PaymentRepository {
	return &PaymentRepository{
		payments: make(map[string]paymentdomain.Payment),
	}
}
