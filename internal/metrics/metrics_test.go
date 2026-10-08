package metrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"
)

// payflow_build_info отдаётся с версией в метке и значением 1. Сверяем
// точный текст вместе с HELP и TYPE: справка - часть контракта метрики.
func TestRegister_BuildInfo(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	if err := register(reg, "v1.2.3"); err != nil {
		t.Fatalf("register: %v", err)
	}

	const want = `
# HELP payflow_build_info Build information about payflow. The value is always 1.
# TYPE payflow_build_info gauge
payflow_build_info{version="v1.2.3"} 1
`
	// Имя метрики обязательно: без него сравнивались бы и все go_* и
	// process_*, а их значения меняются от запуска к запуску.
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "payflow_build_info"); err != nil {
		t.Error(err)
	}
}

func TestRegister_EmptyVersion(t *testing.T) {
	if err := register(prometheus.NewPedanticRegistry(), ""); !errors.Is(err, ErrEmptyVersion) {
		t.Fatalf("want ErrEmptyVersion, got %v", err)
	}
}

// Повторная регистрация в тот же реестр - ошибка, а не паника.
func TestRegister_TwiceReturnsError(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	if err := register(reg, "v1"); err != nil {
		t.Fatalf("first register: %v", err)
	}

	err := register(reg, "v1")
	var are prometheus.AlreadyRegisteredError
	if !errors.As(err, &are) {
		t.Fatalf("want AlreadyRegisteredError, got %v", err)
	}
}

func scrape(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandler_ServesMetrics(t *testing.T) {
	reg, err := NewRegistry("test")
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	rec := scrape(t, Handler(reg, zap.NewNop()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type %q, want text/plain", ct)
	}
	body := rec.Body.String()
	for _, name := range []string{
		"go_goroutines",              // Go-коллектор
		"process_start_time_seconds", // process-коллектор
		`payflow_build_info{version="test"} 1`,
	} {
		if !strings.Contains(body, name) {
			t.Errorf("response has no %s", name)
		}
	}
}

// InstrumentMetricHandler на месте: скрейп учтён в счётчике по коду ответа.
// HELP взят из client_golang; если библиотека его поменяет, тест подскажет.
func TestHandler_CountsScrapes(t *testing.T) {
	reg, err := NewRegistry("test")
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	scrape(t, Handler(reg, zap.NewNop()))

	const want = `
# HELP promhttp_metric_handler_requests_total Total number of scrapes by HTTP status code.
# TYPE promhttp_metric_handler_requests_total counter
promhttp_metric_handler_requests_total{code="200"} 1
promhttp_metric_handler_requests_total{code="500"} 0
promhttp_metric_handler_requests_total{code="503"} 0
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "promhttp_metric_handler_requests_total"); err != nil {
		t.Error(err)
	}
}

// Обработчик можно собрать на одном реестре дважды: promhttp переиспользует
// уже зарегистрированные метрики. Тест ловит смену поведения при обновлении
// библиотеки.
func TestHandler_TwiceOnSameRegistry(t *testing.T) {
	reg, err := NewRegistry("test")
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	Handler(reg, zap.NewNop())
	if rec := scrape(t, Handler(reg, zap.NewNop())); rec.Code != http.StatusOK {
		t.Fatalf("second handler: status %d", rec.Code)
	}
}
