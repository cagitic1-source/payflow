package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var httpDurationBuckets = []float64{
	0.0005, 0.001, 0.0025, 0.005, 0.01,
	0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5,
}

// HTTPMetrics собирает метрики HTTP API: гистограмму длительности запросов
// payflow_http_request_duration_seconds{method,route,code} и число запросов
// в обработке payflow_http_requests_in_flight.
type HTTPMetrics struct {
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

// NewHTTPMetrics создаёт метрики HTTP и регистрирует их в reg. Возвращает
// ошибку, если метрики с такими именами в reg уже зарегистрированы.
func NewHTTPMetrics(reg prometheus.Registerer) (*HTTPMetrics, error) {
	m := &HTTPMetrics{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "payflow",
			Subsystem: "http",
			Name:      "request_duration_seconds",

			Help:    "Duration of HTTP requests by method, route and status code",
			Buckets: httpDurationBuckets,
		}, []string{"method", "route", "code"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "payflow",
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "Number of HTTP requests currently being served.",
		}),
	}
	for _, c := range []prometheus.Collector{
		m.duration, m.inFlight,
	} {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("register %T: %w", c, err)
		}
	}
	return m, nil
}

// Middleware замеряет каждый запрос к next. В route пишется шаблон маршрута
// из ServeMux без метода, поэтому разные id дают одну серию; запрос без
// маршрута попадает в route="unmatched", нестандартный метод - в
// method="other". Если next паникует, запрос учитывается с кодом 500, а
// паника идёт дальше.
//
// ServeMux записывает шаблон в r.Pattern того же запроса, поэтому между
// Middleware и ServeMux не должно быть middleware, которые подменяют запрос
// (r.WithContext, как RequestID), иначе route всегда будет "unmatched".
func (m *HTTPMetrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			m.inFlight.Inc()
			start := time.Now()
			rw := newResponseWrite(w)
			comleted := false

			defer func() {
				m.inFlight.Dec()
				code := rw.statusCode()
				if !comleted {
					code = http.StatusInternalServerError
				}
				m.duration.
					WithLabelValues(methodLabel(r.Method), routeLabel(r), strconv.Itoa(code)).
					Observe(float64(time.Since(start).Seconds()))
			}()
			next.ServeHTTP(rw, r)
			comleted = true
		})
}

func routeLabel(r *http.Request) string {
	if r.Pattern == "" {
		return "unmatched"
	}
	if _, path, found := strings.Cut(r.Pattern, " "); found {
		return path
	}
	return r.Pattern
}

func methodLabel(m string) string {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut,
		http.MethodDelete, http.MethodHead, http.MethodOptions:
		return m
	}
	return "other"
}
