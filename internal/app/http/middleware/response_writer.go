package middleware

import "net/http"

type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

func (w *responseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) statusCode() int {
	if !w.wroteHeader {
		return http.StatusOK
	}
	return w.status
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
