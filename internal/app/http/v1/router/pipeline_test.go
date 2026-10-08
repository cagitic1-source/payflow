package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/payment/memory"
	"github.com/cagitic1-source/payflow/features/payment/service"
	"github.com/cagitic1-source/payflow/features/payment/transport/httptransport"
	"github.com/cagitic1-source/payflow/internal/app/http/v1/router"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	"github.com/cagitic1-source/payflow/internal/core/errors/paymenterrors"
	"github.com/cagitic1-source/payflow/internal/workerpool"
)

// stubBank - банк-заглушка без задержки: отвечает заданным исходом и
// считает обращения по каждому платежу. Если gate задан, ответ ждёт,
// пока его закроют.
type stubBank struct {
	decision service.Decision
	err      error
	gate     chan struct{}

	mu    sync.Mutex
	calls map[string]int // id платежа - сколько раз его авторизовали
}

func (b *stubBank) Authorize(ctx context.Context, p paymentdomain.Payment) (service.Decision, error) {
	b.mu.Lock()
	if b.calls == nil {
		b.calls = make(map[string]int)
	}
	b.calls[p.ID]++
	b.mu.Unlock()

	if b.gate != nil {
		select {
		case <-b.gate:
		case <-ctx.Done():
			return service.Decision{}, ctx.Err()
		}
	}
	return b.decision, b.err
}

// assertCharged проверяет, что банк авторизовал n разных платежей,
// каждый ровно один раз.
func (b *stubBank) assertCharged(t *testing.T, n int) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.calls) != n {
		t.Errorf("bank saw %d payments, want %d", len(b.calls), n)
	}
	for id, c := range b.calls {
		if c != 1 {
			t.Errorf("payment %s authorized %d times, want 1", id, c)
		}
	}
}

// pipeline - HTTP API поверх настоящих сервиса, хранилища, пула и Processor,
// собранных так же, как в main. Заглушка только банк.
type pipeline struct {
	api  http.Handler
	pool *workerpool.Pool
}

func newPipeline(bank service.Acquirer, workers, capacity int) pipeline {
	log := zap.NewNop()
	repo := memory.NewPaymentRepository()
	processor := service.NewProcessor(repo, bank, time.Second, log)
	pool := workerpool.New(workers, capacity, processor.Process, log)
	svc := service.NewPaymentService(repo, memory.NewIdempotencyStore(time.Hour), pool, log)
	return pipeline{
		api:  router.New(log, httptransport.NewPaymentHandler(svc, log)),
		pool: pool,
	}
}

// create создаёт платёж через POST и возвращает его Location.
func (p pipeline) create(t *testing.T, key string) string {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/payments",
		strings.NewReader(`{"merchant_id":"m-1","amount_minor":100,"currency":"RUB"}`))
	req.Header.Set("Idempotency-Key", key)
	rec := httptest.NewRecorder()
	p.api.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("create %s: status %d, want 202; body: %s", key, rec.Code, rec.Body.String())
	}
	assertPaymentBody(t, rec, "pending/")
	return rec.Header().Get("Location")
}

// get читает платёж через GET по его Location.
func (p pipeline) get(t *testing.T, location string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, location, nil)
	rec := httptest.NewRecorder()
	p.api.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get %s: status %d, want 200; body: %s", location, rec.Code, rec.Body.String())
	}
	return rec
}

func paymentStatus(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode payment: %v; body: %s", err, rec.Body.String())
	}
	return body.Status
}

// Путь из README: POST - 202 и pending, платёж обрабатывается в фоне,
// GET по Location показывает итоговый статус.
func TestPipeline_PaymentReachesFinalStatus(t *testing.T) {
	tests := []struct {
		name string
		bank *stubBank
		want string // status/failure_reason
	}{
		{"approved", &stubBank{decision: service.Decision{Approved: true}}, "approved/"},
		{"declined", &stubBank{decision: service.Decision{Reason: "insufficient_funds"}}, "declined/insufficient_funds"},
		{"bank unavailable", &stubBank{err: paymenterrors.ErrAcquirerUnavailable}, "failed/" + service.ReasonAcquirerUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// synctest.Wait дожидается, пока воркеры обработают платёж и
			// снова встанут на пустой очереди, - без sleep и опроса.
			synctest.Test(t, func(t *testing.T) {
				p := newPipeline(tt.bank, 2, 10)

				location := p.create(t, "key-1")
				synctest.Wait()
				assertPaymentBody(t, p.get(t, location), tt.want)
				tt.bank.assertCharged(t, 1)

				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				if err := p.pool.Stop(ctx); err != nil {
					t.Fatalf("stop: %v", err)
				}
			})
		})
	}
}

// Stop при полной очереди: каждый принятый платёж доходит до итогового
// статуса, банк авторизует каждый ровно один раз.
func TestPipeline_StopDrainsQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const workers, n = 4, 50
		bank := &stubBank{decision: service.Decision{Approved: true}, gate: make(chan struct{})}
		p := newPipeline(bank, workers, n)

		locations := make([]string, n)
		for i := range n {
			locations[i] = p.create(t, fmt.Sprintf("key-%d", i))
		}

		// Банк держит ответ: воркеры заняты первыми платежами, остальные
		// ждут в очереди. Без этой проверки тест прошёл бы и тогда, когда
		// воркеры успели всё обработать ещё до Stop.
		synctest.Wait()
		got := map[string]int{}
		for _, loc := range locations {
			got[paymentStatus(t, p.get(t, loc))]++
		}
		if want := map[string]int{"processing": workers, "pending": n - workers}; !maps.Equal(got, want) {
			t.Fatalf("before stop: statuses %v, want %v", got, want)
		}

		stopped := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			stopped <- p.pool.Stop(ctx)
		}()
		synctest.Wait() // Stop закрыл очередь и ждёт воркеров

		close(bank.gate)
		if err := <-stopped; err != nil {
			t.Fatalf("stop: %v", err)
		}

		for _, loc := range locations {
			assertPaymentBody(t, p.get(t, loc), "approved/")
		}
		bank.assertCharged(t, n)
	})
}
