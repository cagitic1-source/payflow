package service_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/cagitic1-source/payflow/features/payment/memory"
	"github.com/cagitic1-source/payflow/features/payment/service"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
)

// barrierRepo задерживает Get, пока его не вызовут оба воркера. Так оба
// гарантированно прочитают платёж в статусе pending до того, как кто-то
// из них успеет его изменить.
type barrierRepo struct {
	*memory.PaymentRepository
	gets sync.WaitGroup
}

func (r *barrierRepo) Get(ctx context.Context, id string) (paymentdomain.Payment, error) {
	p, err := r.PaymentRepository.Get(ctx, id)
	r.gets.Done()
	r.gets.Wait()
	return p, err
}

// countingAcquirer одобряет любой платёж и считает обращения к банку.
type countingAcquirer struct {
	calls atomic.Int64
}

func (a *countingAcquirer) Authorize(context.Context, paymentdomain.Payment) (service.Decision, error) {
	a.calls.Add(1)
	return service.Decision{Approved: true}, nil
}

// Два воркера одновременно взяли один и тот же платёж. В банк должен уйти
// один запрос, иначе деньги спишут дважды.
func TestProcessor_ConcurrentSamePaymentChargesOnce(t *testing.T) {
	repo := &barrierRepo{PaymentRepository: memory.NewPaymentRepository()}
	repo.gets.Add(2)

	p, err := paymentdomain.NewPayment("pay-1", "merchant1", 100, "RUB", time.Now())
	if err != nil {
		t.Fatalf("new payment: %v", err)
	}
	if err := repo.Save(t.Context(), p); err != nil {
		t.Fatalf("save: %v", err)
	}

	acq := &countingAcquirer{}
	pr := service.NewProcessor(repo, acq, time.Second, zap.NewNop())

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { pr.Process(t.Context(), p.ID) })
	}
	wg.Wait()

	if n := acq.calls.Load(); n != 1 {
		t.Fatalf("Authorize called %d times, want 1", n)
	}
	// Читаем мимо барьера: он рассчитан ровно на два Get.
	got, err := repo.PaymentRepository.Get(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != paymentdomain.StatusApproved {
		t.Errorf("status = %s, want %s", got.Status, paymentdomain.StatusApproved)
	}
}

// stubAcquirer отвечает заранее заданным решением или ждёт отмены ctx.
type stubAcquirer struct {
	decision service.Decision
	err      error
	hang     bool   // ждать, пока истечёт ctx
	onCall   func() // если задан, вызывается в начале Authorize
	calls    int
}

func (a *stubAcquirer) Authorize(ctx context.Context, _ paymentdomain.Payment) (service.Decision, error) {
	a.calls++
	if a.onCall != nil {
		a.onCall()
	}
	if a.hang {
		<-ctx.Done()
		return service.Decision{}, ctx.Err()
	}
	return a.decision, a.err
}

func pendingPayment(t *testing.T, repo *fakeRepo) string {
	t.Helper()
	p, err := paymentdomain.NewPayment("pay-1", "m-1", 100, "RUB", time.Now())
	if err != nil {
		t.Fatalf("new payment: %v", err)
	}
	if err := repo.Save(t.Context(), p); err != nil {
		t.Fatalf("save: %v", err)
	}
	return p.ID
}

func TestProcessor_FinalStatuses(t *testing.T) {
	tests := []struct {
		name       string
		acquirer   *stubAcquirer
		wantStatus paymentdomain.Status
		wantReason string
	}{
		{"approved", &stubAcquirer{decision: service.Decision{Approved: true}}, paymentdomain.StatusApproved, ""},
		{"declined", &stubAcquirer{decision: service.Decision{Reason: "insufficient_funds"}}, paymentdomain.StatusDeclined, "insufficient_funds"},
		{"acquirer unavailable", &stubAcquirer{err: errors.New("connection refused")}, paymentdomain.StatusFailed, service.ReasonAcquirerUnavailable},
		{"acquirer timeout", &stubAcquirer{hang: true}, paymentdomain.StatusFailed, service.ReasonAcquirerTimeout},
		{"declined without reason", &stubAcquirer{decision: service.Decision{}}, paymentdomain.StatusDeclined, service.ReasonUnknownDecline},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			id := pendingPayment(t, repo)
			pr := service.NewProcessor(repo, tt.acquirer, 20*time.Millisecond, zap.NewNop())

			pr.Process(t.Context(), id)

			got, _ := repo.Get(t.Context(), id)
			if got.Status != tt.wantStatus || got.FailureReason != tt.wantReason {
				t.Errorf("got (%s, %q), want (%s, %q)", got.Status, got.FailureReason, tt.wantStatus, tt.wantReason)
			}
		})
	}
}

// Пул остановили, пока запрос был у банка. Если банк успел ответить, его
// ответ сохраняется: деньги могли уже списать. Итог пишется уже после
// отмены ctx, а fakeRepo, как база, на отменённом ctx не пишет: если Process
// сохранит итог без context.WithoutCancel, платёж застрянет в processing.
func TestProcessor_ShutdownWhileWaitingForBank(t *testing.T) {
	tests := []struct {
		name       string
		acquirer   *stubAcquirer
		wantStatus paymentdomain.Status
		wantReason string
	}{
		{"bank did not answer", &stubAcquirer{hang: true}, paymentdomain.StatusFailed, service.ReasonProcessingInterrupted},
		{"bank approved", &stubAcquirer{decision: service.Decision{Approved: true}}, paymentdomain.StatusApproved, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			id := pendingPayment(t, repo)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			tt.acquirer.onCall = cancel // пул останавливают, пока запрос у банка
			pr := service.NewProcessor(repo, tt.acquirer, time.Second, zap.NewNop())

			pr.Process(ctx, id)

			got, err := repo.Get(t.Context(), id)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Status != tt.wantStatus || got.FailureReason != tt.wantReason {
				t.Errorf("got (%s, %q), want (%s, %q)", got.Status, got.FailureReason, tt.wantStatus, tt.wantReason)
			}
		})
	}
}

func TestProcessor_SkipsPaymentThatIsNotPending(t *testing.T) {
	repo := &fakeRepo{}
	id := pendingPayment(t, repo)
	acq := &stubAcquirer{decision: service.Decision{Approved: true}}
	pr := service.NewProcessor(repo, acq, time.Second, zap.NewNop())

	pr.Process(t.Context(), id) // первая обработка: approved
	pr.Process(t.Context(), id) // повторная доставка того же id

	if acq.calls != 1 {
		t.Fatalf("acquirer called %d times, want 1 (no double charge)", acq.calls)
	}
}

func TestProcessor_LeavesPaymentPendingOnShutdown(t *testing.T) {
	repo := &fakeRepo{}
	id := pendingPayment(t, repo)
	acq := &stubAcquirer{decision: service.Decision{Approved: true}}
	pr := service.NewProcessor(repo, acq, time.Second, zap.NewNop())

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // пул остановлен принудительно

	pr.Process(ctx, id)

	got, _ := repo.Get(t.Context(), id)
	if got.Status != paymentdomain.StatusPending || acq.calls != 0 {
		t.Errorf("got status %s, acquirer calls %d; want pending and 0 calls", got.Status, acq.calls)
	}
}
