// Package router_test contains tests for the v1 HTTP router.
package router_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/payment/memory"
	"github.com/cagitic1-source/payflow/features/payment/service"
	"github.com/cagitic1-source/payflow/features/payment/transport/httptransport"
	"github.com/cagitic1-source/payflow/internal/app/http/middleware"
	"github.com/cagitic1-source/payflow/internal/app/http/v1/router"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

func newRouter() http.Handler {
	log := zap.NewNop()
	svc := service.NewPaymentService(memory.NewPaymentRepository(), memory.NewIdempotencyStore(time.Hour), acceptQueue{})
	return router.New(log, httptransport.NewPaymentHandler(svc, log))
}

func TestAPI(t *testing.T) {
	created := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	stored := paymentdomain.Payment{
		ID: "pay-1", MerchantID: "m-1", AmountMinor: 100, Currency: "RUB",
		Status: paymentdomain.StatusPending, CreatedAt: created, UpdatedAt: created,
	}

	declined := stored
	declined.Status = paymentdomain.StatusDeclined
	declined.FailureReason = "insufficient_funds"

	validBody := `{"merchant_id":"m-1","amount_minor":100,"currency":"RUB"}`
	withKey := map[string]string{"Idempotency-Key": "key-1"}

	tests := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
		body    string

		svcPayment  paymentdomain.Payment
		svcReplayed bool
		svcErr      error

		wantStatus      int
		wantType        string // для ответов-ошибок: последняя часть type или "about:blank"
		wantLocation    string
		wantReplayed    bool   // ждём заголовок Idempotent-Replayed: true
		wantRetryAfter  string // ждём заголовок Retry-After с этим значением
		wantBody        string // для успешных ответов: ожидаемые status и failure_reason через "/"
		wantServiceCall bool   // должен ли запрос дойти до сервиса
	}{
		{
			name: "create: body too large", method: http.MethodPost, path: "/v1/payments",
			headers:    withKey,
			body:       `{"merchant_id":"` + strings.Repeat("a", 1<<20) + `","amount_minor":100,"currency":"RUB"}`,
			wantStatus: http.StatusBadRequest, wantType: "malformed-request",
		},
		{
			name: "create: service overloaded", method: http.MethodPost, path: "/v1/payments",
			headers: withKey, body: validBody, svcErr: paymenterrors.ErrOverloaded,
			wantStatus: http.StatusServiceUnavailable, wantType: "service-overloaded",
			wantRetryAfter: "1", wantServiceCall: true,
		},
		{
			name: "get: declined with reason", method: http.MethodGet, path: "/v1/payments/pay-1",
			svcPayment: declined,
			wantStatus: http.StatusOK, wantBody: "declined/insufficient_funds",
		},
		{
			name: "create: success", method: http.MethodPost, path: "/v1/payments",
			headers: withKey, body: validBody, svcPayment: stored,
			wantStatus: http.StatusAccepted, wantLocation: "/v1/payments/pay-1", wantServiceCall: true, wantBody: "pending/",
		},
		{
			name: "create: replay", method: http.MethodPost, path: "/v1/payments",
			headers: withKey, body: validBody, svcPayment: stored, svcReplayed: true,
			wantStatus: http.StatusAccepted, wantLocation: "/v1/payments/pay-1", wantReplayed: true, wantServiceCall: true,
		},
		{
			name: "create: missing idempotency key", method: http.MethodPost, path: "/v1/payments",
			body:       validBody,
			wantStatus: http.StatusBadRequest, wantType: "missing-idempotency-key",
		},
		{
			name: "create: idempotency key too long", method: http.MethodPost, path: "/v1/payments",
			headers: map[string]string{"Idempotency-Key": strings.Repeat("k", 256)}, body: validBody,
			wantStatus: http.StatusBadRequest, wantType: "missing-idempotency-key",
		},
		{
			name: "create: idempotency key reused", method: http.MethodPost, path: "/v1/payments",
			headers: withKey, body: validBody,
			svcErr:     fmt.Errorf("reserve idempotency key: %w", paymenterrors.ErrIdempotencyKeyReused),
			wantStatus: http.StatusUnprocessableEntity, wantType: "idempotency-key-reused", wantServiceCall: true,
		},
		{
			name: "create: idempotency request in progress", method: http.MethodPost, path: "/v1/payments",
			headers: withKey, body: validBody,
			svcErr:     fmt.Errorf("reserve idempotency key: %w", paymenterrors.ErrIdempotencyInProgress),
			wantStatus: http.StatusConflict, wantType: "idempotency-request-in-progress", wantServiceCall: true,
		},
		{
			name: "create: malformed JSON", method: http.MethodPost, path: "/v1/payments",
			body: `{`, headers: withKey,
			wantStatus: http.StatusBadRequest, wantType: "malformed-request",
		},
		{
			name: "create: unknown field", method: http.MethodPost, path: "/v1/payments",
			body: `{"merchant_id":"m-1","amount":100,"currency":"RUB"}`, headers: withKey,
			wantStatus: http.StatusBadRequest, wantType: "malformed-request",
		},
		{
			name: "create: trailing data", method: http.MethodPost, path: "/v1/payments",
			body: validBody + validBody, headers: withKey,
			wantStatus: http.StatusBadRequest, wantType: "malformed-request",
		},
		{
			name: "create: validation error from service", method: http.MethodPost, path: "/v1/payments",
			body: validBody, headers: withKey, svcErr: fmt.Errorf("create payment: %w", paymenterrors.ErrInvalidAmount),
			wantStatus: http.StatusUnprocessableEntity, wantType: "invalid-amount", wantServiceCall: true,
		},
		{
			name: "create: internal error", method: http.MethodPost, path: "/v1/payments",
			body: validBody, headers: withKey, svcErr: errors.New("db is down"),
			wantStatus: http.StatusInternalServerError, wantType: "about:blank", wantServiceCall: true,
		},

		// --- GET /v1/payments/{id} ---
		{
			name: "get: found", method: http.MethodGet, path: "/v1/payments/pay-1",
			svcPayment: stored,
			wantStatus: http.StatusOK, wantBody: "pending/",
		},
		{
			name: "get: not found", method: http.MethodGet, path: "/v1/payments/nope",
			svcErr:     fmt.Errorf("get payment: %w", paymenterrors.ErrNotFound),
			wantStatus: http.StatusNotFound, wantType: "payment-not-found",
		},

		// --- GET /healthz ---
		{
			name: "healthz", method: http.MethodGet, path: "/healthz",
			wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			svc := &fakePaymentService{
				payment:  tt.svcPayment,
				replayed: tt.svcReplayed,
				err:      tt.svcErr,
			}
			log := zap.NewNop()
			api := router.New(log, httptransport.NewPaymentHandler(svc, log))

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, strings.NewReader(tt.body))
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			api.ServeHTTP(rec, req)

			if got := rec.Header().Get("Retry-After"); got != tt.wantRetryAfter {
				t.Errorf("Retry-After header: got %q, want %q", got, tt.wantRetryAfter)
			}
			if tt.wantBody != "" {
				assertPaymentBody(t, rec, tt.wantBody)
			}

			if rec.Code != tt.wantStatus {
				t.Fatalf("status: got %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if rec.Header().Get(middleware.RequestIDHeader) == "" {
				t.Error("response has no request id header")
			}
			if tt.wantLocation != "" && rec.Header().Get("Location") != tt.wantLocation {
				t.Errorf("location: got %q, want %q", rec.Header().Get("Location"), tt.wantLocation)
			}
			if tt.wantType != "" {
				assertProblemType(t, rec, tt.wantType)
			}
			if got := rec.Header().Get("Idempotent-Replayed") == "true"; got != tt.wantReplayed {
				t.Errorf("Idempotent-Replayed header: got %v, want %v", got, tt.wantReplayed)
			}
			if tt.method == http.MethodPost {
				if tt.wantServiceCall != (svc.createCalls > 0) {
					t.Errorf("service called %d times, want called: %v", svc.createCalls, tt.wantServiceCall)
				}
				// Транспорт должен передать в сервис именно тот ключ, что пришёл в заголовке.
				if tt.wantServiceCall && svc.lastCmd.IdempotencyKey != tt.headers["Idempotency-Key"] {
					t.Errorf("idempotency key passed to service: got %q, want %q",
						svc.lastCmd.IdempotencyKey, tt.headers["Idempotency-Key"])
				}
			}
		})
	}
}

// assertPaymentBody проверяет status и failure_reason в теле ответа о платеже.
// want - строка вида "declined/insufficient_funds" или "pending/".
func assertPaymentBody(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()

	var body struct {
		Status        string    `json:"status"`
		FailureReason string    `json:"failure_reason"`
		CreatedAt     time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode payment: %v; body: %s", err, rec.Body.String())
	}
	if got := body.Status + "/" + body.FailureReason; got != want {
		t.Errorf("status/failure_reason: got %q, want %q", got, want)
	}
	if body.CreatedAt.IsZero() {
		t.Error("created_at is missing")
	}
	if strings.Contains(rec.Body.String(), "failure_reason") && body.FailureReason == "" {
		t.Error("empty failure_reason must be omitted from JSON")
	}
}

// assertProblemType проверяет Content-Type и поле type ответа-ошибки.
func assertProblemType(t *testing.T, rec *httptest.ResponseRecorder, wantType string) {
	t.Helper()

	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type: got %q, want application/problem+json", ct)
	}

	var p struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem: %v; body: %s", err, rec.Body.String())
	}

	want := wantType
	if wantType != "about:blank" {
		want = "https://payflow.dev/problems/" + wantType
	}
	if p.Type != want {
		t.Errorf("problem type: got %q, want %q", p.Type, want)
	}
}

// Повтор POST с тем же ключом через настоящие сервис и хранилище.
func TestAPI_CreatePaymentIdempotency(t *testing.T) {
	api := newRouter()
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/payments", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "key-1")
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	paymentID := func(rec *httptest.ResponseRecorder) string {
		t.Helper()
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("decode payment: %v; body: %s", err, rec.Body.String())
		}
		return p.ID
	}
	body := `{"merchant_id":"m-1","amount_minor":100,"currency":"RUB"}`

	first := post(body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first: status %d, want 202; body: %s", first.Code, first.Body.String())
	}
	if got := first.Header().Get(httptransport.IdempotentReplayedHeader); got != "" {
		t.Errorf("first: %s = %q, want no header", httptransport.IdempotentReplayedHeader, got)
	}

	replay := post(body)
	if replay.Code != http.StatusAccepted {
		t.Fatalf("replay: status %d, want 202; body: %s", replay.Code, replay.Body.String())
	}
	if got := replay.Header().Get(httptransport.IdempotentReplayedHeader); got != "true" {
		t.Errorf("replay: %s = %q, want \"true\"", httptransport.IdempotentReplayedHeader, got)
	}
	if first, replay := paymentID(first), paymentID(replay); first != replay {
		t.Errorf("replay created another payment: %s, first was %s", replay, first)
	}
	if got, want := replay.Header().Get("Location"), first.Header().Get("Location"); got != want {
		t.Errorf("replay location: got %q, want %q", got, want)
	}

	reused := post(`{"merchant_id":"m-1","amount_minor":999,"currency":"RUB"}`)
	if reused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("reused key: status %d, want 422; body: %s", reused.Code, reused.Body.String())
	}
	assertProblemType(t, reused, "idempotency-key-reused")
}

func TestRouter_Routes(t *testing.T) {
	h := newRouter()

	tests := []struct {
		method, path string
		wantStatus   int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/v1/unknown", http.StatusNotFound},
		{http.MethodDelete, "/v1/payments/unknown", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("want status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body)
			}
			if rec.Header().Get(middleware.RequestIDHeader) == "" {
				t.Errorf("want %s header to be set", middleware.RequestIDHeader)
			}
		})
	}
}

func TestRouter_KeepsIncomingRequestID(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	req.Header.Set(middleware.RequestIDHeader, "req-123")

	rec := httptest.NewRecorder()
	newRouter().ServeHTTP(rec, req)

	if got := rec.Header().Get(middleware.RequestIDHeader); got != "req-123" {
		t.Errorf("want request id req-123, got %q", got)
	}
}
