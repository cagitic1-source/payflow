package main

import (
	"fmt"
	"os"
	"time"

	"github.com/cagitic1-source/payflow/internal/app/http/v1/server"
)

// config - настройки запуска. Читаются из переменных окружения:
//
//	PAYFLOW_ADDR                 адрес сервера, по умолчанию ":8080"
//	PAYFLOW_READ_HEADER_TIMEOUT  server.Config.ReadHeaderTimeout
//	PAYFLOW_READ_TIMEOUT         server.Config.ReadTimeout
//	PAYFLOW_WRITE_TIMEOUT        server.Config.WriteTimeout
//	PAYFLOW_IDLE_TIMEOUT         server.Config.IdleTimeout
//	PAYFLOW_SHUTDOWN_TIMEOUT     server.Config.ShutdownTimeout
//
// Таймауты задаются в формате time.ParseDuration ("10s", "1m"). Незаданная
// или пустая переменная - значение по умолчанию из server.DefaultConfig.
type config struct {
	addr   string
	server server.Config
}

func loadConfig() (config, error) {
	c := config{
		addr:   ":8080",
		server: server.DefaultConfig(),
	}
	if v := os.Getenv("PAYFLOW_ADDR"); v != "" {
		c.addr = v
	}

	timeouts := []struct {
		env string
		dst *time.Duration
	}{
		{"PAYFLOW_READ_HEADER_TIMEOUT", &c.server.ReadHeaderTimeout},
		{"PAYFLOW_READ_TIMEOUT", &c.server.ReadTimeout},
		{"PAYFLOW_WRITE_TIMEOUT", &c.server.WriteTimeout},
		{"PAYFLOW_IDLE_TIMEOUT", &c.server.IdleTimeout},
		{"PAYFLOW_SHUTDOWN_TIMEOUT", &c.server.ShutdownTimeout},
	}
	for _, t := range timeouts {
		v := os.Getenv(t.env)
		if v == "" {
			continue
		}
		// Ноль у http.Server значит «без таймаута», поэтому принимаем только > 0.
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return config{}, fmt.Errorf("%s=%q: want positive duration like 10s", t.env, v)
		}
		*t.dst = d
	}
	return c, nil
}
