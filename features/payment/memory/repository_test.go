package memory_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/cagitic1-source/payflow/features/payment/memory"
	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
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
