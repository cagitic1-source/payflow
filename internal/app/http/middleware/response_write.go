package middleware

import "net/http"

type responseWrite struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func newResponseWrite(w http.ResponseWriter) *responseWrite {
	return &responseWrite{ResponseWriter: w}
}

func (w *responseWrite) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWrite) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *responseWrite) statusCode() int {
	if !w.wroteHeader {
		return http.StatusOK
	}
	return w.status
}

func (w *responseWrite) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
