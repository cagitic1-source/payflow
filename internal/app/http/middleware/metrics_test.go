package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/internal/app/http/middleware"
)

const durationMetric = "payflow_http_request_duration_seconds"

// newChain собирает цепочку как в роутере: RequestID -> Metrics -> Recover -> mux.
func newChain(t *testing.T, mux *http.ServeMux) (http.Handler, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	m, err := middleware.NewHTTPMetrics(reg)
	if err != nil {
		t.Fatalf("new http metrics: %v", err)
	}
	log := zap.NewNop()
	return middleware.RequestID(log, m.Middleware(middleware.Recover(log, mux))), reg
}

func serve(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))
	return rec
}

// family возвращает серии метрики name из реестра.
func family(t *testing.T, reg prometheus.Gatherer, name string) []*dto.Metric {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() == name {
			return f.GetMetric()
		}
	}
	return nil
}

// requestCount - сколько запросов записано в серию с этими метками.
func requestCount(t *testing.T, reg prometheus.Gatherer, method, route, code string) uint64 {
	t.Helper()
	for _, m := range family(t, reg, durationMetric) {
		labels := map[string]string{}
		for _, l := range m.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}
		if labels["method"] == method && labels["route"] == route && labels["code"] == code {
			return m.GetHistogram().GetSampleCount()
		}
	}
	return 0
}

func inFlight(t *testing.T, reg prometheus.Gatherer) float64 {
	t.Helper()
	series := family(t, reg, "payflow_http_requests_in_flight")
	if len(series) != 1 {
		t.Fatalf("want one in_flight series, got %d", len(series))
	}
	return series[0].GetGauge().GetValue()
}

func TestHTTPMetrics_StatusCode(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"explicit WriteHeader", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
		}, "422"},
		{"Write without WriteHeader is 200", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}, "200"},
		{"empty response is 200", func(http.ResponseWriter, *http.Request) {}, "200"},
		{"first WriteHeader wins", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			w.WriteHeader(http.StatusInternalServerError)
		}, "201"},
		{"panic before response is 500", func(http.ResponseWriter, *http.Request) {
			panic("boom")
		}, "500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /x", tt.handler)
			h, reg := newChain(t, mux)

			serve(t, h, http.MethodGet, "/x")

			if got := requestCount(t, reg, "GET", "/x", tt.want); got != 1 {
				t.Errorf("code %s: got %d requests, want 1", tt.want, got)
			}
		})
	}
}

// Паника после начала ответа: Recover не дописывает 500 к обрывку, а
// обрывает соединение; метрика всё равно видит ошибку сервера.
func TestHTTPMetrics_PanicAfterResponseStarted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial")) // код 200 уже ушёл клиенту
		panic("boom")
	})
	h, reg := newChain(t, mux)
	rec := httptest.NewRecorder()

	func() {
		defer func() {
			err, _ := recover().(error)
			if !errors.Is(err, http.ErrAbortHandler) {
				t.Errorf("want ErrAbortHandler to abort the connection, got %v", err)
			}
		}()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))
	}()

	if body := rec.Body.String(); body != "partial" {
		t.Errorf("body = %q: nothing must be written after the response started", body)
	}
	if got := requestCount(t, reg, "GET", "/x", "500"); got != 1 {
		t.Errorf("want the aborted request counted as 500, got %d", got)
	}
	if got := inFlight(t, reg); got != 0 {
		t.Errorf("in_flight = %v after panic, want 0", got)
	}
}

// Разные id - один шаблон маршрута и одна серия.
func TestHTTPMetrics_RouteIsPattern(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/payments/{id}", func(http.ResponseWriter, *http.Request) {})
	h, reg := newChain(t, mux)

	for i := range 100 {
		serve(t, h, http.MethodGet, "/v1/payments/p"+strings.Repeat("x", i))
	}

	if n, err := testutil.GatherAndCount(reg, durationMetric); err != nil || n != 1 {
		t.Fatalf("want 1 series, got %d (err %v)", n, err)
	}
	if got := requestCount(t, reg, "GET", "/v1/payments/{id}", "200"); got != 100 {
		t.Errorf("got %d requests, want 100", got)
	}
}

// Путь и метод выбирает клиент: несовпавшие запросы и неизвестные методы
// не должны порождать новые значения меток.
func TestHTTPMetrics_UnboundedLabelsAreCollapsed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/payments/{id}", func(http.ResponseWriter, *http.Request) {})
	h, reg := newChain(t, mux)

	tests := []struct {
		method, path         string
		wantMethod, wantCode string
	}{
		{http.MethodGet, "/wp-admin/login.php", "GET", "404"},
		{http.MethodPost, "/v1/payments/p1", "POST", "405"},
		{"FOO", "/v1/payments/p1", "other", "405"},
	}
	for _, tt := range tests {
		serve(t, h, tt.method, tt.path)
		if got := requestCount(t, reg, tt.wantMethod, "unmatched", tt.wantCode); got != 1 {
			t.Errorf("%s %s: want 1 request with method=%s route=unmatched code=%s, got %d",
				tt.method, tt.path, tt.wantMethod, tt.wantCode, got)
		}
	}
}

func TestHTTPMetrics_InFlight(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x", func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})
	h, reg := newChain(t, mux)

	done := make(chan struct{})
	go func() {
		defer close(done)
		serve(t, h, http.MethodGet, "/x")
	}()

	<-started
	if got := inFlight(t, reg); got != 1 {
		t.Errorf("in_flight = %v while serving, want 1", got)
	}
	close(release)
	<-done
	if got := inFlight(t, reg); got != 0 {
		t.Errorf("in_flight = %v after request, want 0", got)
	}
}

// Обёртка не должна прятать возможности исходного ResponseWriter:
// ResponseController находит его через Unwrap.
func TestHTTPMetrics_ResponseControllerReachesUnderlyingWriter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x", func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush through wrapper: %v", err)
		}
	})
	h, _ := newChain(t, mux)

	if rec := serve(t, h, http.MethodGet, "/x"); !rec.Flushed {
		t.Error("underlying writer was not flushed")
	}
}

// Корзины подобраны под payflow: первая на 0,5 мс, иначе ответы API за доли
// миллисекунды все попадут в одну корзину и квантили потеряют смысл.
// Тест фиксирует набор: менять его - осознанное решение.
func TestHTTPMetrics_Buckets(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x", func(http.ResponseWriter, *http.Request) {})
	h, reg := newChain(t, mux)
	serve(t, h, http.MethodGet, "/x")

	series := family(t, reg, durationMetric)
	if len(series) != 1 {
		t.Fatalf("want 1 series, got %d", len(series))
	}
	var got []float64
	for _, b := range series[0].GetHistogram().GetBucket() {
		got = append(got, b.GetUpperBound())
	}
	want := []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}
	if !slices.Equal(got, want) {
		t.Errorf("buckets = %v, want %v", got, want)
	}
}

func TestNewHTTPMetrics_TwiceReturnsError(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	if _, err := middleware.NewHTTPMetrics(reg); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := middleware.NewHTTPMetrics(reg)
	var are prometheus.AlreadyRegisteredError
	if !errors.As(err, &are) {
		t.Fatalf("want AlreadyRegisteredError, got %v", err)
	}
}
