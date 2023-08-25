package grouter

import (
	"net/http"
)

type ResponseWriter struct {
	responseWriter http.ResponseWriter
	StatusCode     *int
}

func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{
		responseWriter: w,
		StatusCode:     nil,
	}
}

func (w *ResponseWriter) Write(p []byte) (n int, err error) {
	return w.responseWriter.Write(p)
}

func (w *ResponseWriter) WriteHeader(statusCode int) {
	w.responseWriter.WriteHeader(statusCode)
	w.StatusCode = &statusCode
}

func (w *ResponseWriter) Header() http.Header {
	return w.responseWriter.Header()
}
