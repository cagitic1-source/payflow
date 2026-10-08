// Package httptransport_test contains tests for the payment HTTP transport.
package httptransport_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/payment/memory"
	"github.com/cagitic1-source/payflow/features/payment/service"
	"github.com/cagitic1-source/payflow/features/payment/transport/httptransport"
)

func newService() *service.PaymentService {
	return service.NewPaymentService(memory.NewPaymentRepository(), memory.NewIdempotencyStore(time.Hour), acceptQueue{}, zap.NewNop())
}

func newMux(svc httptransport.PaymentService) *http.ServeMux {
	h := httptransport.NewPaymentHandler(svc, zap.NewNop())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /payments/{id}", h.GetPayment)
	return mux
}

func TestGetPayment_Found(t *testing.T) {
	svc := newService()
	created, err := svc.CreatePayment(t.Context(), service.CreatePaymentCommand{
		MerchantID: "merchant1", AmountMinor: 1000, Currency: "USD", IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	rec := httptest.NewRecorder()
	newMux(svc).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/payments/"+created.Payment.ID, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("want status 200, got %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("want Content-Type application/json, got %q", ct)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := map[string]any{
		"id":           created.Payment.ID,
		"merchant_id":  "merchant1",
		"amount_minor": float64(1000),
		"currency":     "USD",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("field %q: want %v, got %v", k, v, body[k])
		}
	}
}

func TestGetPayment_NotFound(t *testing.T) {
	svc := newService()

	rec := httptest.NewRecorder()
	newMux(svc).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/payments/unknown", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want status 404, got %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("want Content-Type application/problem+json, got %q", ct)
	}

	var p struct {
		Type     string `json:"type"`
		Status   int    `json:"status"`
		Instance string `json:"instance"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if p.Type != "https://payflow.dev/problems/payment-not-found" {
		t.Errorf("unexpected problem type %q", p.Type)
	}
	if p.Status != http.StatusNotFound {
		t.Errorf("want problem status 404, got %d", p.Status)
	}
	if p.Instance != "/payments/unknown" {
		t.Errorf("want instance /payments/unknown, got %q", p.Instance)
	}
}

func TestGetPayment_InternalError(t *testing.T) {
	svc := &fakePaymentService{err: errors.New("db is down")}

	rec := httptest.NewRecorder()
	newMux(svc).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/payments/some-id", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want status 500, got %d: %s", rec.Code, rec.Body)
	}
}
