package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

func sanitizeLogURI(rawURI string) string {
	idx := strings.IndexByte(rawURI, '?')
	if idx == -1 {
		return rawURI
	}
	pathPart := rawURI[:idx]
	queryPart := rawURI[idx+1:]
	values, err := url.ParseQuery(queryPart)
	if err != nil {
		return pathPart + "?[REDACTED]"
	}
	for key := range values {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "key") || strings.Contains(lower, "sig") ||
			strings.Contains(lower, "auth") || strings.Contains(lower, "pass") ||
			strings.HasPrefix(lower, "x-amz-") {
			values[key] = []string{"[REDACTED]"}
		}
	}
	return pathPart + "?" + values.Encode()
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 16)
}

type bufferPool struct {
	pool sync.Pool
	size int
}

func newBufferPool(size int) *bufferPool {
	if size != 32<<10 && size != 64<<10 && size != 128<<10 {
		size = 64 << 10
	}
	return &bufferPool{size: size}
}

func (p *bufferPool) Get() []byte {
	if value := p.pool.Get(); value != nil {
		return *(value.(*[]byte))
	}
	buffer := make([]byte, p.size)
	return buffer
}

func (p *bufferPool) Put(value []byte) {
	if cap(value) < p.size {
		return
	}
	value = value[:p.size]
	p.pool.Put(&value)
}

type captureWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
	selected    any
	requestID   string
}

func (w *captureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *captureWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *captureWriter) Write(value []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(value)
	w.bytes += int64(written)
	return written, err
}

func (w *captureWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func responseWriterFromContext(ctx context.Context) (*captureWriter, bool) {
	value, ok := ctx.Value(writerContextKey{}).(*captureWriter)
	return value, ok
}

var _ httputil.BufferPool = (*bufferPool)(nil)
var _ io.Writer = (*captureWriter)(nil)
