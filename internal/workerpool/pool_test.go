package workerpool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// submit занимает место и ставит задачу в очередь; падает, если места нет.
func submit(t *testing.T, p *Pool, id string) {
	t.Helper()
	if !p.TryAcquire() {
		t.Fatalf("no slot for %s", id)
	}
	p.Enqueue(id)
}

func stop(t *testing.T, p *Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestPool_ProcessesAllJobs(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}

	p := New(4, 100, func(_ context.Context, id string) {
		mu.Lock()
		seen[id]++
		mu.Unlock()
	}, zap.NewNop())

	for i := range 100 {
		submit(t, p, fmt.Sprintf("job-%d", i))
	}
	stop(t, p) // Stop дожидается обработки всей очереди

	if len(seen) != 100 {
		t.Fatalf("processed %d distinct jobs, want 100", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s processed %d times, want 1", id, n)
		}
	}
}

func TestPool_RunsWorkersInParallelUpToLimit(t *testing.T) {
	const workers = 4
	var running, maxRunning atomic.Int64
	release := make(chan struct{})

	p := New(workers, 20, func(_ context.Context, _ string) {
		n := running.Add(1)
		for { // запоминаем максимум одновременно работающих обработчиков
			m := maxRunning.Load()
			if n <= m || maxRunning.CompareAndSwap(m, n) {
				break
			}
		}
		<-release // держим обработчик, пока тест не отпустит
		running.Add(-1)
	}, zap.NewNop())

	for i := range 10 {
		submit(t, p, fmt.Sprintf("job-%d", i))
	}

	// Ждём, пока все воркеры заняты.
	deadline := time.Now().Add(2 * time.Second)
	for running.Load() < workers && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	stop(t, p)

	if got := maxRunning.Load(); got != workers {
		t.Fatalf("max parallel handlers = %d, want %d", got, workers)
	}
}

func TestPool_TryAcquireRespectsCapacity(t *testing.T) {
	block := make(chan struct{})
	p := New(1, 3, func(_ context.Context, _ string) { <-block }, zap.NewNop())

	for i := range 3 {
		submit(t, p, fmt.Sprintf("job-%d", i))
	}
	if p.TryAcquire() {
		t.Fatal("TryAcquire succeeded beyond capacity")
	}

	close(block)
	stop(t, p)
}

// Из одновременных TryAcquire ровно capacity должны получить место.
func TestPool_TryAcquireConcurrent(t *testing.T) {
	const capacity = 10
	p := New(1, capacity, func(context.Context, string) {}, zap.NewNop())
	defer stop(t, p)

	var won atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if p.TryAcquire() {
				won.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if won.Load() != capacity {
		t.Fatalf("acquired %d slots, want %d", won.Load(), capacity)
	}
	for range capacity {
		p.Release()
	}
}

func TestPool_StopRespectsDeadline(t *testing.T) {
	sawCancel := make(chan struct{})
	p := New(1, 1, func(ctx context.Context, _ string) {
		<-ctx.Done() // долгая задача, которая уважает ctx
		close(sawCancel)
	}, zap.NewNop())
	submit(t, p, "slow")

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := p.Stop(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Stop took %v, want about 50ms", elapsed)
	}
	select {
	case <-sawCancel:
	default:
		t.Error("handler did not see context cancellation")
	}
}

func TestPool_PanicDoesNotKillWorkerOrLeakSlot(t *testing.T) {
	var processed atomic.Int64
	p := New(1, 1, func(_ context.Context, id string) {
		if id == "boom" {
			panic("handler failed")
		}
		processed.Add(1)
	}, zap.NewNop())

	submit(t, p, "boom")
	// Единственный воркер должен пережить панику и вернуть единственное место.
	deadline := time.Now().Add(2 * time.Second)
	for !p.TryAcquire() {
		if time.Now().After(deadline) {
			t.Fatal("slot was not released after panic")
		}
		time.Sleep(time.Millisecond)
	}
	p.Enqueue("ok")
	stop(t, p)

	if processed.Load() != 1 {
		t.Fatalf("job after panic processed %d times, want 1", processed.Load())
	}
}

func TestPool_RejectsAfterStop(t *testing.T) {
	p := New(1, 1, func(context.Context, string) {}, zap.NewNop())
	stop(t, p)

	if p.TryAcquire() {
		t.Fatal("TryAcquire succeeded after Stop")
	}
}
