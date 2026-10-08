// Package workerpool запускает фиксированное число воркеров над очередью задач с ограниченным числом мест.
package workerpool

import (
	"context"
	"errors"
	"sync"

	"go.uber.org/zap"
)

// ErrStopped - пул остановлен, задача не принята.
var ErrStopped = errors.New("worker pool is stopped")

// Handler обрабатывает одну задачу. Должен уважать ctx: при принудительной
// остановке пула ctx отменяется, и обработчик обязан быстро завершиться.
type Handler func(ctx context.Context, id string)

// Pool - фиксированное число воркеров и ограниченное число мест.
//
// Место (слот) занимается через TryAcquire до постановки задачи в очередь
// и освобождается, когда воркер закончил её обработку. Ёмкость очереди
// равна числу слотов, поэтому Enqueue после успешного TryAcquire никогда
// не блокируется.
type Pool struct {
	slots   chan struct{}
	queue   chan string
	handler Handler
	log     *zap.Logger

	ctx    context.Context // отменяется при принудительной остановке
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.RWMutex // защищает closed и закрытие queue
	closed bool
}

// New запускает пул из workers воркеров, в котором одновременно может
// находиться не больше capacity задач (в очереди и в обработке).
func New(workers, capacity int, handler Handler, log *zap.Logger) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		slots:   make(chan struct{}, capacity),
		queue:   make(chan string, capacity),
		handler: handler,
		log:     log,
		ctx:     ctx,
		cancel:  cancel,
	}
	p.wg.Add(workers)
	for range workers {
		go p.worker()
	}
	return p
}

// TryAcquire занимает место, не блокируясь. false - мест нет или пул остановлен.
func (p *Pool) TryAcquire() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return false
	}

	select {
	case p.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// Enqueue ставит задачу в очередь. Вызывать только после успешного TryAcquire.
// Место переходит пулу в любом случае: после обработки его вернёт воркер,
// а если пул уже остановлен - сам Enqueue, вместе с ErrStopped.
func (p *Pool) Enqueue(id string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		// Пул остановили между TryAcquire и Enqueue: задача не будет
		// обработана, место нужно вернуть.
		p.Release()
		return ErrStopped
	}
	p.queue <- id
	return nil
}

// Stop перестаёт принимать задачи и ждёт, пока воркеры обработают очередь.
// Если ctx истёк раньше, отменяет контекст обработчиков, дожидается их
// выхода и возвращает ошибку ctx.
func (p *Pool) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.queue)
	}

	p.mu.Unlock()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.cancel()
		return nil
	case <-ctx.Done():
		p.cancel() // обработчики видят отмену и завершаются
		<-done
		return ctx.Err()
	}
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for id := range p.queue {
		p.process(id)
	}
}

// process обрабатывает одну задачу. Паника в обработчике не должна ни
// убить воркер, ни навсегда занять место.
func (p *Pool) process(id string) {
	defer p.Release()
	defer func() {
		if rec := recover(); rec != nil {
			p.log.Error("worker panic", zap.String("id", id), zap.Any("panic", rec), zap.Stack("stack"))
		}
	}()
	p.handler(p.ctx, id)
}

// Release возвращает место, занятое TryAcquire, если задача так и не была
// поставлена в очередь (например, не удалось её сохранить).
func (p *Pool) Release() {
	<-p.slots
}
