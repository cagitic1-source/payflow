package middleware

import (
	"fmt"
	"net/http"
)

// BodyLimit ограничивает тело запроса maxBytes байтами. Стоит первым в
// цепочке: http.MaxBytesReader должен получить исходный ResponseWriter,
// иначе сервер не узнает о превышении и не закроет соединение после ответа.
// Ответ пишет обработчик: чтение сверх лимита вернёт *http.MaxBytesError.
func BodyLimit(maxBytes int64, next http.Handler) http.Handler {
	if maxBytes <= 0 {
		panic(fmt.Sprintf("middleware.BodyLimit: maxBytes must be positive, got %d", maxBytes))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		next.ServeHTTP(w, r)
	})
}
