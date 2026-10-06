package acquirer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	paymentdomain "github.com/cagitic1-source/payflow/internal/core/domain/payment"
	paymenterrors "github.com/cagitic1-source/payflow/internal/core/errors/payment_errors"
)

// sequence возвращает заданные числа по очереди - детерминированная «случайность».
func sequence(values ...float64) func() float64 {
	i := 0
	return func() float64 {
		v := values[i%len(values)]
		i++
		return v
	}
}

func TestFake_Outcomes(t *testing.T) {
	cfg := FakeConfig{DeclineRate: 0.05, ErrorRate: 0.02} // без задержки

	tests := []struct {
		name         string
		random       []float64 // первое число - исход, второе - выбор причины отказа
		wantApproved bool
		wantReason   string
		wantErr      error
	}{
		{name: "error", random: []float64{0.01}, wantErr: paymenterrors.ErrUnavailable},
		{name: "decline", random: []float64{0.05, 0.0}, wantReason: "insufficient_funds"},
		{name: "decline other reason", random: []float64{0.05, 0.99}, wantReason: "suspected_fraud"},
		{name: "approve", random: []float64{0.5}, wantApproved: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewFake(cfg)
			f.outcome = sequence(tt.random...)

			d, err := f.Authorize(t.Context(), paymentdomain.Payment{})

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("want error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Approved != tt.wantApproved || d.Reason != tt.wantReason {
				t.Errorf("got %+v, want approved=%v reason=%q", d, tt.wantApproved, tt.wantReason)
			}
		})
	}
}

func TestFake_RespectsContextDeadline(t *testing.T) {
	f := NewFake(FakeConfig{MinLatency: time.Second, MaxLatency: time.Second})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := f.Authorize(ctx, paymentdomain.Payment{})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Authorize ignored ctx: returned after %v", elapsed)
	}
}

// Одновременные вызовы безопасны (проверяет -race) и дают только допустимые исходы.
func TestFake_ConcurrentUse(t *testing.T) {
	f := NewFake(FakeConfig{MaxLatency: time.Millisecond, DeclineRate: 0.3, ErrorRate: 0.1})

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := f.Authorize(context.Background(), paymentdomain.Payment{})
			switch {
			case err != nil:
				if !errors.Is(err, paymenterrors.ErrUnavailable) {
					t.Errorf("unexpected error: %v", err)
				}
			case d.Approved && d.Reason != "":
				t.Errorf("approved decision must have no reason, got %q", d.Reason)
			case !d.Approved && d.Reason == "":
				t.Error("declined decision must have a reason")
			}
		}()
	}
	wg.Wait()
}
