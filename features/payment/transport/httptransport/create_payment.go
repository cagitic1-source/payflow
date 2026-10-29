// Package httptransport contains HTTP transport implementations for the payment domain.
package httptransport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/internal/core/errors/paymenterrors"
)

// CreatePayment обрабатывает POST /v1/payments: создаёт платёж и отвечает 202
// с телом платежа и заголовком Location. Невалидное тело или нет заголовка
// Idempotency-Key - 400, ошибки валидации - 422. Повтор с тем же ключом и
// телом получает тот же ответ с заголовком Idempotent-Replayed: true; пока
// первый запрос выполняется - 409, ключ с другим телом - 422.
//
// Размер тела обработчик сам не ограничивает: это делает middleware.BodyLimit
// в цепочке router.New. Чтение сверх лимита возвращает *http.MaxBytesError,
// и клиент получает 413, а не 400: тело может быть валидным, просто большим.
func (h *PaymentHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get(idempotencyKeyHeader)
	if key == "" || len(key) > 255 {
		respondError(w, r, h.log, paymenterrors.ErrEmptyIdempotencyKey)
		return
	}

	var req createPaymentRequest
	if err := decodeBody(r.Body, &req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respondError(w, r, h.log, fmt.Errorf("%w: %w", ErrBodyTooLarge, err))
			return
		}
		respondError(w, r, h.log, fmt.Errorf("%w: %w", ErrMalformedRequest, err))
		return
	}

	cmd := req.toCommand()
	cmd.IdempotencyKey = key
	res, err := h.svc.CreatePayment(r.Context(), cmd)
	if err != nil {
		respondError(w, r, h.log, err)
		return
	}

	log := requestLogger(r, h.log)
	log.Info("payment accepted",
		zap.String("payment_id", res.Payment.ID),
		zap.Bool("replayed", res.Replayed))

	if res.Replayed {
		w.Header().Set(IdempotentReplayedHeader, "true")
	}
	w.Header().Set("Location", "/v1/payments/"+res.Payment.ID)
	writeJSON(w, h.log, http.StatusAccepted, toPaymentResponse(res.Payment))
}

// decodeBody разбирает тело как ровно один JSON-объект без неизвестных полей.
// Ошибки чтения возвращает обёрнутыми через %w: вызывающему нужен
// *http.MaxBytesError, если тело упёрлось в лимит.
func decodeBody(body io.Reader, v any) error {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	switch err := dec.Decode(&struct{}{}); {
	case errors.Is(err, io.EOF):
		return nil
	case err == nil:
		return errors.New("unexpected data after JSON object")
	default:
		// Мусор после объекта или хвост сверх лимита.
		return fmt.Errorf("unexpected data after JSON object: %w", err)
	}
}
