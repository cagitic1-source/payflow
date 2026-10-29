package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsMux(t *testing.T) {
	// Заглушка вместо настоящего обработчика: проверяем только маршруты.
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux := newMetricsMux(ok)

	tests := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/metrics", http.StatusOK},
		{http.MethodHead, "/metrics", http.StatusOK}, // шаблон с GET покрывает и HEAD
		{http.MethodPost, "/metrics", http.StatusMethodNotAllowed},
		{http.MethodGet, "/", http.StatusNotFound},
		{http.MethodGet, "/v1/payments/p1", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestLoadConfig_MetricsAddr(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"default", "", ":8081"},
		{"from env", "127.0.0.1:9100", "127.0.0.1:9100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PAYFLOW_METRICS_ADDR", tt.env)
			cfg, err := loadConfig()
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.metricsAddr != tt.want {
				t.Errorf("metricsAddr %q, want %q", cfg.metricsAddr, tt.want)
			}
		})
	}
}
