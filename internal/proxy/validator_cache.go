package proxy

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

// Local 304s are an optimization for public, fresh representations only.
// Even with authenticated body caching enabled, credentials require a trip
// through the data plane; a local validator must not replace authentication.
func validatorRequestCacheable(repository model.Mirror, request *http.Request) bool {
	if !repository.CacheEnabled || request.Header.Get("Range") != "" ||
		request.Header.Get("If-Match") != "" || request.Header.Get("If-Unmodified-Since") != "" ||
		len(request.Header.Values("Cache-Control")) > 0 || len(request.Header.Values("Pragma")) > 0 {
		return false
	}
	header := request.Header.Clone()
	sanitizeProxyCookies(header)
	if len(header.Values("Authorization")) > 0 || len(header.Values("Cookie")) > 0 {
		return false
	}
	for name, value := range repository.HeaderAdd {
		if value != "" && (strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Cookie")) {
			return false
		}
	}
	return true
}

func (e *Engine) storeMetadataValidator(meta requestMeta, response *http.Response, validator metadataValidator) {
	ttl := e.cfg.Cache.MetadataTTL
	if meta.repository.MetadataTTLSec > 0 {
		ttl = time.Duration(meta.repository.MetadataTTLSec) * time.Second
	}
	now := time.Now()
	ttl, age, cacheable := validatorResponseFreshness(response, now, ttl)
	if !meta.validatorCacheable || !cacheable {
		e.validators.remove(meta.validatorKey)
		return
	}
	validator.StoredAt, validator.InitialAge = now, age
	validator.ExpiresAt = now.Add(ttl)
	validator.Headers = make(http.Header)
	for _, name := range []string{"Cache-Control", "Content-Location", "Date", "Expires", "Vary", "Content-Security-Policy", "X-Frame-Options", "Referrer-Policy", "Permissions-Policy"} {
		if values := response.Header.Values(name); len(values) > 0 {
			validator.Headers[name] = append([]string(nil), values...)
		}
	}
	e.validators.put(meta.validatorKey, validator)
}

func validatorResponseFreshness(response *http.Response, now time.Time, ttl time.Duration) (time.Duration, time.Duration, bool) {
	header := response.Header
	if response.StatusCode != http.StatusOK || len(header.Values("Set-Cookie")) > 0 || len(header.Values("Pragma")) > 0 {
		return 0, 0, false
	}
	for _, value := range header.Values("Vary") {
		for _, name := range strings.Split(value, ",") {
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "", "accept", "accept-encoding":
			default:
				return 0, 0, false
			}
		}
	}
	for _, line := range header.Values("Cache-Control") {
		for _, directive := range strings.Split(line, ",") {
			name, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "no-cache", "no-store", "private", "must-revalidate", "proxy-revalidate":
				return 0, 0, false
			case "max-age", "s-maxage":
				seconds, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(value), "\""), 10, 32)
				if err != nil || seconds <= 0 {
					return 0, 0, false
				}
				ttl = min(ttl, time.Duration(seconds)*time.Second)
			}
		}
	}
	age := time.Duration(0)
	if date, err := http.ParseTime(header.Get("Date")); err == nil && now.After(date) {
		age = now.Sub(date)
	}
	if value := header.Get("Age"); value != "" {
		seconds, err := strconv.ParseInt(value, 10, 32)
		if err != nil || seconds < 0 {
			return 0, 0, false
		}
		age = max(age, time.Duration(seconds)*time.Second)
	}
	ttl -= age
	if value := header.Get("Expires"); value != "" {
		expires, err := http.ParseTime(value)
		if err != nil {
			return 0, 0, false
		}
		ttl = min(ttl, expires.Sub(now))
	}
	return ttl, age, ttl > 0
}
