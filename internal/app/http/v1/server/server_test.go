package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"
)

func listen(t *testing.T) (net.Listener, error) {
	t.Helper()
	var lc net.ListenConfig
	return lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
}

// Запрос, начатый до остановки, должен завершиться успешно.
func TestRun_GracefulShutdownCompletesInFlightRequest(t *testing.T) {
	ln, err := listen(t) // порт 0 - ОС выберет любой свободный
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	started := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)                     // сообщаем тесту: запрос в обработке
		time.Sleep(300 * time.Millisecond) // имитация долгой работы
		_, _ = w.Write([]byte("done"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- Run(ctx, ln, handler, DefaultConfig(), zap.NewNop()) }()

	type response struct {
		body string
		err  error
	}
	respCh := make(chan response, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ln.Addr().String(), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			respCh <- response{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		respCh <- response{body: string(b), err: err}
	}()

	<-started // запрос точно внутри обработчика
	cancel()  // «пришёл SIGTERM»

	r := <-respCh
	if r.err != nil || r.body != "done" {
		t.Fatalf("in-flight request: got (%q, %v), want (\"done\", nil)", r.body, r.err)
	}
	if err := <-runErr; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// После остановки новые соединения не принимаются.
func TestRun_RejectsNewConnectionsAfterShutdown(t *testing.T) {
	ln, err := listen(t)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- Run(ctx, ln, http.NotFoundHandler(), DefaultConfig(), zap.NewNop()) }()

	cancel()
	if err := <-runErr; err != nil {
		t.Fatalf("Run: %v", err)
	}

	d := net.Dialer{Timeout: time.Second}
	if conn, err := d.DialContext(context.Background(), "tcp", addr); err == nil {
		_ = conn.Close()
		t.Fatal("server still accepts connections after shutdown")
	}
}
