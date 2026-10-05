// Package requestctx хранит данные запроса в context.Context.
package requestctx

import (
	"context"

	"go.uber.org/zap"
)

// Ключи - неэкспортируемые типы: другой пакет не сможет создать такой же ключ
// и случайно перезаписать значение.
type requestIDKey struct{}
type loggerKey struct{}

// WithRequestID возвращает копию ctx с id запроса.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID возвращает id запроса из ctx или "", если его туда не клали.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// WithLogger возвращает копию ctx с логгером запроса - обычно уже
// дополненным полем request_id.
func WithLogger(ctx context.Context, log *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, log)
}

// Logger возвращает логгер запроса из ctx или nil, если его туда не клали.
// Вызывающий код должен быть готов к nil.
func Logger(ctx context.Context) *zap.Logger {
	log, _ := ctx.Value(loggerKey{}).(*zap.Logger)
	return log
}
