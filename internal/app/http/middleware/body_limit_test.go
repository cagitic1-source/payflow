package middleware_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cagitic1-source/payflow/internal/app/http/middleware"
)

func TestBodyLimit(t *testing.T) {
	const limit = 10

	tests := []struct {
		name    string
		body    string
		wantErr bool // true - чтение должно упереться в лимит
	}{
		{name: "exactly at limit", body: strings.Repeat("a", limit), wantErr: false},
		{name: "one byte over limit", body: strings.Repeat("a", limit+1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				got     []byte
				readErr error
			)
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got, readErr = io.ReadAll(r.Body)
			})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body))
			middleware.BodyLimit(limit, next).ServeHTTP(httptest.NewRecorder(), req)

			if !tt.wantErr {
				if readErr != nil {
					t.Fatalf("read body: %v", readErr)
				}
				if len(got) != limit {
					t.Errorf("read %d bytes, want %d", len(got), limit)
				}
				return
			}
			var maxErr *http.MaxBytesError
			if !errors.As(readErr, &maxErr) {
				t.Fatalf("want *http.MaxBytesError, got %v", readErr)
			}
			if maxErr.Limit != limit {
				t.Errorf("MaxBytesError.Limit = %d, want %d", maxErr.Limit, limit)
			}
		})
	}
}

func TestBodyLimit_PanicsOnNonPositiveLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int64
	}{
		{name: "zero", limit: 0},
		{name: "negative", limit: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("want panic")
				}
			}()
			middleware.BodyLimit(tt.limit, http.NotFoundHandler())
		})
	}
}
