package middleware

import (
	"encoding/json"
	"errors"
	"net/http"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/internal/core/requestctx"
)

// Recover перехватывает панику в next, логирует её со стеком и отвечает 500
// в формате application/problem+json с request_id.
//
// Логгер берётся из контекста (его кладёт RequestID); log используется,
// только если его там нет. http.ErrAbortHandler не перехватывается —
// это штатный способ оборвать ответ.
func Recover(log *zap.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// ErrAbortHandler — штатный способ оборвать ответ, его не глушим
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}

			// Без RequestID снаружи логгера в контексте нет — берём базовый.
			logger := requestctx.Logger(r.Context())
			if logger == nil {
				logger = log
			}
			logger.Error("panic recovered",
				zap.Any("error", rec),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Stack("stack"),
			)
			writeInternalError(w, r)
		}()

		next.ServeHTTP(w, r)
	})
}

func writeInternalError(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":       "about:blank",
		"title":      http.StatusText(http.StatusInternalServerError),
		"status":     http.StatusInternalServerError,
		"instance":   r.URL.Path,
		"request_id": requestctx.RequestID(r.Context()),
	})
}
