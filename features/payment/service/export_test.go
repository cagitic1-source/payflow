package service

import "time"

// SetNow подменяет часы сервиса
func (s *PaymentService) SetNow(now func() time.Time) {
	s.now = now
}
