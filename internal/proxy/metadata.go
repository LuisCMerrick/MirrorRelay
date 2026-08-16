package proxy

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/RepoGate/internal/config"
	"github.com/LuisCMerrick/RepoGate/internal/model"
)

var absoluteURLPattern = regexp.MustCompile(`https?://[^\s"'<>\\]+`)

type metadataValidator struct {
	ETag         string
	LastModified string
	ExpiresAt    time.Time
}

type metadataValidators struct {
	mu      sync.Mutex
	entries map[string]metadataValidator
	order   []string
	maximum int
}

func newMetadataValidators(maximum int) *metadataValidators {
	if maximum <= 0 {
		maximum = 2048
	}
	return &metadataValidators{entries: make(map[string]metadataValidator), maximum: maximum}
}

func (v *metadataValidators) get(key string, now time.Time) (metadataValidator, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.entries[key]
	if !ok || !entry.ExpiresAt.After(now) {
		if ok {
			delete(v.entries, key)
		}
		return metadataValidator{}, false
	}
	return entry, true
}

func (v *metadataValidators) put(key string, entry metadataValidator) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, exists := v.entries[key]; !exists {
		v.order = append(v.order, key)
	}
	v.entries[key] = entry
	for len(v.entries) > v.maximum && len(v.order) > 0 {
		oldest := v.order[0]
		v.order = v.order[1:]
		delete(v.entries, oldest)
	}
	if len(v.order) > v.maximum*2 {
		compacted := v.order[:0]
		for _, candidate := range v.order {
			if _, exists := v.entries[candidate]; exists {
				compacted = append(compacted, candidate)
			}
		}
		v.order = compacted
	}
}

type gzipPool struct {
	pool sync.Pool
}

func (p *gzipPool) compress(source []byte) ([]byte, error) {
	var output bytes.Buffer
	var writer *gzip.Writer
	if pooled := p.pool.Get(); pooled != nil {
		writer = pooled.(*gzip.Writer)
		writer.Reset(&output)
	} else {
		writer, _ = gzip.NewWriterLevel(&output, gzip.DefaultCompression)
	}
	writer.Header.ModTime = time.Time{}
	writer.Header.Name = ""
	writer.Header.Comment = ""
	_, writeErr := writer.Write(source)
	closeErr := writer.Close()
	p.pool.Put(writer)
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return output.Bytes(), nil
}

func shouldRewriteBody(response *http.Response) bool {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	return strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/json") ||
		strings.Contains(contentType, "+json") || strings.Contains(contentType, "application/vnd.pypi.simple")
}

func rewriteResponseBody(response *http.Response, repository model.Mirror, publicBase string, cfg config.MetadataConfig, acceptsGzip bool, compressors *gzipPool) (metadataValidator, error) {
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return metadataValidator{}, &unexpectedMetadataEncodingError{encoding: encoding}
	}
	limit := cfg.RewriteBufferLimit
	if response.ContentLength > limit {
		return metadataValidator{}, &rewriteTooLargeError{limit: limit}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return metadataValidator{}, err
	}
	if int64(len(body)) > limit {
		return metadataValidator{}, &rewriteTooLargeError{limit: limit}
	}
	_ = response.Body.Close()
	rewritten := absoluteURLPattern.ReplaceAllFunc(body, func(raw []byte) []byte {
		value := string(raw)
		if !rewriteURLAllowed(repository, value) {
			return raw
		}
		return []byte(publicBase + publicFetchPath(repository, value))
	})

	output := rewritten
	encoding := "identity"
	compressionMayVary := cfg.OutputCompression == "auto" || cfg.OutputCompression == "gzip"
	if compressionMayVary && acceptsGzip && len(rewritten) >= cfg.GzipMinLength {
		output, err = compressors.compress(rewritten)
		if err != nil {
			return metadataValidator{}, err
		}
		encoding = "gzip"
	}
	sum := sha256.New()
	_, _ = sum.Write([]byte("mirror-metadata-v5\x00" + repository.ProfileName + "\x00" + repository.ProfileVersion + "\x00" + repository.RewriteProfile + "\x00" + publicBase + "\x00" + encoding + "\x00"))
	_, _ = sum.Write(output)
	etag := `"repogate-v5-` + hex.EncodeToString(sum.Sum(nil)) + `"`
	response.Body = io.NopCloser(bytes.NewReader(output))
	response.ContentLength = int64(len(output))
	response.Header.Set("Content-Length", strconv.FormatInt(response.ContentLength, 10))
	response.Header.Set("ETag", etag)
	response.Header.Del("Content-Encoding")
	response.Header.Del("Content-Range")
	response.Header.Del("Last-Modified")
	response.Header.Del("Content-MD5")
	response.Header.Del("Digest")
	response.Header.Del("Content-Digest")
	response.Header.Del("Repr-Digest")
	if encoding == "gzip" {
		response.Header.Set("Content-Encoding", "gzip")
	}
	if compressionMayVary {
		addVary(response.Header, "Accept-Encoding")
	}
	return metadataValidator{ETag: etag}, nil
}

func sanitizeRewrittenMetadataHead(response *http.Response) {
	response.ContentLength = -1
	for _, name := range []string{
		"Content-Length", "Content-Encoding", "Content-Range", "Accept-Ranges",
		"ETag", "Last-Modified", "Content-MD5", "Digest", "Content-Digest", "Repr-Digest",
	} {
		response.Header.Del(name)
	}
	addVary(response.Header, "Accept-Encoding")
}

type rewriteTooLargeError struct {
	limit int64
}

type unexpectedMetadataEncodingError struct{ encoding string }

func (e *unexpectedMetadataEncodingError) Error() string {
	return "rewrite metadata upstream ignored identity encoding request and returned " + e.encoding
}

func (e *rewriteTooLargeError) Error() string {
	return "rewrite metadata exceeds " + strconv.FormatInt(e.limit, 10) + " bytes"
}

func rewriteURLAllowed(repository model.Mirror, raw string) bool {
	parsed, err := parseAbsoluteHTTPURL(raw)
	return err == nil && isAllowedRewriteOrigin(repository, parsed)
}

func addVary(header http.Header, value string) {
	for _, line := range header.Values("Vary") {
		for _, item := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func acceptsGzip(value string) bool {
	for _, part := range strings.Split(value, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") && strings.TrimSpace(fields[0]) != "*" {
			continue
		}
		allowed := true
		for _, parameter := range fields[1:] {
			name, raw, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(name, "q") && strings.TrimSpace(raw) == "0" {
				allowed = false
			}
		}
		if allowed {
			return true
		}
	}
	return false
}

func conditionMatches(request *http.Request, validator metadataValidator) bool {
	if values := request.Header.Values("If-None-Match"); len(values) > 0 {
		for _, line := range values {
			for _, candidate := range strings.Split(line, ",") {
				candidate = strings.TrimSpace(candidate)
				if candidate == "*" || weakETagEqual(candidate, validator.ETag) {
					return true
				}
			}
		}
		return false
	}
	if validator.LastModified == "" {
		return false
	}
	modified, modifiedErr := http.ParseTime(validator.LastModified)
	since, sinceErr := http.ParseTime(request.Header.Get("If-Modified-Since"))
	return modifiedErr == nil && sinceErr == nil && !modified.After(since.Add(time.Second))
}

func weakETagEqual(left, right string) bool {
	left = strings.TrimSpace(strings.TrimPrefix(left, "W/"))
	right = strings.TrimSpace(strings.TrimPrefix(right, "W/"))
	return left != "" && left == right
}
