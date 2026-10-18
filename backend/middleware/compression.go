package middleware

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// Compression gzip-compresses textual responses when the client advertises
// support. Images, fonts, range responses, and already-encoded responses are
// passed through untouched.
func Compression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) || r.Method == http.MethodHead || r.Header.Get("Range") != "" {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Add("Vary", "Accept-Encoding")
		writer := &compressionWriter{ResponseWriter: w, status: http.StatusOK}
		defer writer.Close()
		next.ServeHTTP(writer, r)
	})
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]), "gzip") {
			return true
		}
	}
	return false
}

func compressibleContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if contentType == "" {
		return false
	}
	if contentType == "text/event-stream" {
		return false
	}
	return strings.HasPrefix(contentType, "text/") ||
		contentType == "application/json" ||
		contentType == "application/javascript" ||
		contentType == "application/xml" ||
		contentType == "application/xhtml+xml" ||
		contentType == "image/svg+xml"
}

type compressionWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	decided     bool
	compress    bool
	gzipWriter  *gzip.Writer
}

func (w *compressionWriter) WriteHeader(status int) {
	if w.decided {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.wroteHeader = true
}

func (w *compressionWriter) Write(body []byte) (int, error) {
	w.decide(body)
	if w.compress {
		return w.gzipWriter.Write(body)
	}
	return w.ResponseWriter.Write(body)
}

func (w *compressionWriter) decide(body []byte) {
	if w.decided {
		return
	}
	w.decided = true

	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	if w.Header().Get("Content-Type") == "" && len(body) > 0 {
		w.Header().Set("Content-Type", http.DetectContentType(body))
	}

	if w.Header().Get("Content-Encoding") != "" ||
		status < http.StatusOK ||
		status == http.StatusNoContent ||
		status == http.StatusNotModified ||
		status == http.StatusPartialContent ||
		!compressibleContentType(w.Header().Get("Content-Type")) {
		w.ResponseWriter.WriteHeader(status)
		return
	}

	w.compress = true
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Encoding", "gzip")
	w.ResponseWriter.WriteHeader(status)
	w.gzipWriter = gzip.NewWriter(w.ResponseWriter)
}

func (w *compressionWriter) Close() {
	if w.gzipWriter != nil {
		_ = w.gzipWriter.Close()
		return
	}
	// A handler that explicitly wrote only a status still needs that status
	// sent. A handler that wrote nothing is left alone so an outer recoverer
	// can still produce a 500.
	if w.wroteHeader {
		w.decide(nil)
	}
}

func (w *compressionWriter) Flush() {
	w.decide(nil)
	if w.gzipWriter != nil {
		_ = w.gzipWriter.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *compressionWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
