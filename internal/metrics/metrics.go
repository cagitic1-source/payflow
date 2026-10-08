// Package metrics собирает метрики payflow для Prometheus и отдаёт их по HTTP.
package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// namespace - общий префикс всех метрик payflow: payflow_build_info,
// payflow_http_request_duration_seconds и т. д.
const namespace = "payflow"

const (
	// maxScrapesInFlight ограничивает одновременные запросы к /metrics.
	// Prometheus приходит раз в scrape_interval, так что параллельных
	// скрейпов почти не бывает. Лимит защищает от того, кто начнёт
	// долбить /metrics: сверх лимита promhttp отвечает 503.
	maxScrapesInFlight = 3

	// scrapeTimeout должен быть меньше scrape_timeout в prometheus.yml.
	// Тогда при медленном сборе мы сами ответим 503 с понятной причиной,
	// а не Prometheus оборвёт соединение по своему таймауту.
	scrapeTimeout = 3 * time.Second
)

// NewRegistry создаёт реестр с метриками Go, процесса и payflow_build_info.
func NewRegistry(version string) (*prometheus.Registry, error) {
	reg := prometheus.NewRegistry()
	if err := register(reg, version); err != nil {
		return nil, err
	}
	return reg, nil
}

// register регистрирует стандартные метрики payflow в reg. Вынесена
// отдельно, чтобы тесты могли передать педантичный реестр.
func register(reg prometheus.Registerer, version string) error {
	if version == "" {
		return ErrEmptyVersion
	}

	// Info-метрика: данные в метке, значение всегда 1.
	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace:   namespace,
		Name:        "build_info",
		Help:        "Build information about payflow. The value is always 1.",
		ConstLabels: prometheus.Labels{"version": version},
	})
	buildInfo.Set(1)

	for _, c := range []prometheus.Collector{
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		buildInfo,
	} {
		if err := reg.Register(c); err != nil {
			return fmt.Errorf("register %T: %w", c, err)
		}
	}
	return nil
}

// Handler отдаёт метрики из реестра по HTTP.
func Handler(reg *prometheus.Registry, log *zap.Logger) http.Handler {
	h := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorLog: errorLogger{log: log},
		// Счётчик promhttp_metric_handler_errors_total: ошибки, которые
		// не видны по HTTP-коду, например сбой после начала ответа.
		Registry:            reg,
		MaxRequestsInFlight: maxScrapesInFlight,
		Timeout:             scrapeTimeout,
	})
	return promhttp.InstrumentMetricHandler(reg, h)
}

// errorLogger передаёт ошибки promhttp в zap. promhttp нужен только
// метод Println, и вызывает он его вида Println("error gathering metrics:", err).
type errorLogger struct {
	log *zap.Logger
}

func (l errorLogger) Println(v ...any) {
	// Sprintln, а не Sprint: Sprint не ставит пробел между строкой и
	// ошибкой. TrimSpace убирает перевод строки в конце.
	l.log.Error("serve metrics", zap.String("error", strings.TrimSpace(fmt.Sprintln(v...))))
}
