package memory_test

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cagitic1-source/payflow/features/payment/memory"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

func TestPaymentRepository_ConcurrentSave(t *testing.T) {
	repo := memory.NewPaymentRepository()
	const n = 100

	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			p := paymentdomain.Payment{ID: fmt.Sprintf("id-%d", i), MerchantID: "m-1", AmountMinor: 100, Currency: "RUB"}
			if err := repo.Save(t.Context(), p); err != nil {
				t.Errorf("Save failed: %v", err)
			}
		})
	}
	wg.Wait()

	for i := range n {
		id := fmt.Sprintf("id-%d", i)
		got, err := repo.Get(t.Context(), id)
		if err != nil {
			t.Errorf("GetByID failed for %s: %v", id, err)
		}
		if got.ID != id {
			t.Errorf("GetByID returned wrong payment for %s: got %+v", id, got)
		}
	}

}

// Из одновременных попыток взять платёж в обработку успешна ровно одна.
func TestPaymentRepository_UpdateIfStatusConcurrent(t *testing.T) {
	repo := memory.NewPaymentRepository()
	p := paymentdomain.Payment{ID: "pay-1", Status: paymentdomain.StatusPending}
	if err := repo.Save(t.Context(), p); err != nil {
		t.Fatalf("save: %v", err)
	}
	processing := p
	processing.Status = paymentdomain.StatusProcessing

	const n = 50
	var won, lost atomic.Int64
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			err := repo.UpdateIfStatus(t.Context(), processing, paymentdomain.StatusPending)
			switch {
			case err == nil:
				won.Add(1)
			case errors.Is(err, paymenterrors.ErrInvalidTransition):
				lost.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
	wg.Wait()

	if won.Load() != 1 || lost.Load() != n-1 {
		t.Fatalf("won %d, lost %d; want 1 and %d", won.Load(), lost.Load(), n-1)
	}
}

func TestPaymentRepository_UpdateIfStatusNotFound(t *testing.T) {
	repo := memory.NewPaymentRepository()
	p := paymentdomain.Payment{ID: "missing", Status: paymentdomain.StatusProcessing}

	err := repo.UpdateIfStatus(t.Context(), p, paymentdomain.StatusPending)
	if !errors.Is(err, paymenterrors.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
