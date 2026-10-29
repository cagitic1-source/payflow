package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/payment/transport/httptransport"
	"github.com/cagitic1-source/payflow/internal/app/http/v1/router"
)

// Тело сверх лимита: сервер отвечает 413 и закрывает соединение
// (Connection: close). Закрывает он его, только если http.MaxBytesReader
// получил исходный ResponseWriter, то есть BodyLimit стоит в цепочке первым.
// Рекордер этого не покажет, поэтому нужен настоящий сервер.
//
// Превышение - 10 КиБ: больше нуля, чтобы лимит сработал, и меньше 256 КиБ.
// Если непрочитанный остаток тела больше 256 КиБ, net/http закрывает
// соединение сам, и тест перестаёт отличать правильную цепочку от
// неправильной.
//
// Ключ идемпотентности и валидное начало JSON нужны, чтобы обработчик
// дочитал тело до лимита. Без ключа он отвечает 400, не читая тела, а на
// невалидном JSON декодер останавливается на первых байтах. В обоих случаях
// остаток больше 256 КиБ, и соединение закрывается при любой цепочке.
//
// Тест идёт около полсекунды: после превышения лимита сервер ждёт
// rstAvoidanceDelay перед закрытием соединения, а srv.Close ждёт сервер.
func TestAPI_BodyOverLimitClosesConnection(t *testing.T) {
	const overLimit = 10 << 10

	log := zap.NewNop()
	srv := httptest.NewServer(router.New(log, newTestHTTPMetrics(), httptransport.NewPaymentHandler(&fakePaymentService{}, log)))
	defer srv.Close()

	body := `{"merchant_id":"` + strings.Repeat("a", router.MaxRequestBodyBytes+overLimit) + `","amount_minor":100,"currency":"RUB"}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/v1/payments", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Idempotency-Key", "key-1")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	if !resp.Close {
		t.Error("server must close the connection after the body limit is hit (Connection: close)")
	}
}
