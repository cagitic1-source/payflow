// Package middleware contains HTTP middleware shared by all API versions.
package middleware

import (
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/internal/core/requestctx"
)

// RequestIDHeader - заголовок, в котором id запроса приходит от клиента
// и возвращается в ответе.
const RequestIDHeader = "X-Request-ID"

// Принимаем от клиента только короткие id из безопасных символов (UUID подходит).
// Всё остальное заменяем своим: защита от log injection и огромных значений.
var validRequestID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// RequestID присваивает запросу id: берёт его из X-Request-ID, если он валиден,
// иначе генерирует UUIDv7. Id возвращается клиенту в том же заголовке и кладётся
// в контекст вместе с логгером, у которого уже есть поле request_id
// (см. requestctx.RequestID и requestctx.Logger).
//
// Должен стоять снаружи Recover, иначе в логе паники не будет request_id.
func RequestID(log *zap.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}

		w.Header().Set(RequestIDHeader, id)
		ctx := requestctx.WithRequestID(r.Context(), id)
		ctx = requestctx.WithLogger(ctx, log.With(zap.String("request_id", id)))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}
