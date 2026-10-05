package middleware_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/cagitic1-source/payflow/internal/app/http/middleware"
	"github.com/cagitic1-source/payflow/internal/core/requestctx"
)

func TestRequestID(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		wantPassed bool // true - id из заголовка должен сохраниться
	}{
		{name: "no header", header: "", wantPassed: false},
		{name: "valid header", header: "abc-123_XYZ", wantPassed: true},
		{name: "too long", header: strings.Repeat("a", 65), wantPassed: false},
		{name: "log injection", header: "abc\n{\"level\":\"error\"}", wantPassed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.InfoLevel)

			var ctxID string
			h := middleware.RequestID(zap.New(core), http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				ctxID = requestctx.RequestID(r.Context())
				requestctx.Logger(r.Context()).Info("inside handler")
			}))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set(middleware.RequestIDHeader, tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			got := rec.Header().Get(middleware.RequestIDHeader)
			if got != ctxID {
				t.Errorf("response header %q != context id %q", got, ctxID)
			}
			if tt.wantPassed && got != tt.header {
				t.Errorf("valid id must pass through: got %q, want %q", got, tt.header)
			}
			if !tt.wantPassed {
				id, err := uuid.Parse(got)
				if err != nil || id.Version() != 7 {
					t.Errorf("want generated UUIDv7, got %q", got)
				}
			}

			entries := logs.All()
			if len(entries) != 1 || entries[0].ContextMap()["request_id"] != got {
				t.Errorf("log entry must carry request_id %q, got %+v", got, entries)
			}
		})
	}
}

func TestRecover_Panic(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	log := zap.New(core)
	h := middleware.RequestID(log, middleware.Recover(log, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/payments/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type: got %q", ct)
	}

	var body struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.RequestID == "" || body.RequestID != rec.Header().Get(middleware.RequestIDHeader) {
		t.Errorf("body request_id %q must match header %q", body.RequestID, rec.Header().Get(middleware.RequestIDHeader))
	}

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("want 1 log entry, got %d", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["request_id"] != body.RequestID {
		t.Errorf("log request_id: got %v, want %q", fields["request_id"], body.RequestID)
	}
	if stack, _ := fields["stack"].(string); stack == "" {
		t.Error("log entry must contain stack")
	}
}

func TestRecover_AbortHandlerPropagates(t *testing.T) {
	h := middleware.Recover(zap.NewNop(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		err, _ := recover().(error)
		if !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("want ErrAbortHandler to propagate, got %v", err)
		}
	}()

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	t.Error("panic was swallowed")
}
