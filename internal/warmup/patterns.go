package warmup

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	maxWarmupURLPatterns = 1024
	maxWarmupURLPattern  = 4096
)

// NormalizeURLPatterns validates bounded repository-relative request targets
// and removes duplicates while retaining their original order.
func NormalizeURLPatterns(patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, errors.New("at least one URL pattern is required")
	}
	if len(patterns) > maxWarmupURLPatterns {
		return nil, fmt.Errorf("URL patterns must not exceed %d entries", maxWarmupURLPatterns)
	}
	result := make([]string, 0, len(patterns))
	seen := make(map[string]bool, len(patterns))
	for index, raw := range patterns {
		normalized, err := normalizeURLPattern(raw)
		if err != nil {
			return nil, fmt.Errorf("URL pattern %d: %w", index+1, err)
		}
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}
	return result, nil
}

func normalizeURLPattern(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("must not be empty")
	}
	if len(value) > maxWarmupURLPattern {
		return "", fmt.Errorf("must not exceed %d bytes", maxWarmupURLPattern)
	}
	if strings.ContainsAny(value, "\x00\r\n\\") {
		return "", errors.New("must not contain control characters or backslashes")
	}
	if parsed, err := url.Parse(value); err != nil || parsed.IsAbs() {
		return "", errors.New("must be a repository-relative path")
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	if strings.HasPrefix(value, "//") {
		return "", errors.New("must be a repository-relative path")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("must be a valid repository-relative request URI")
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil || strings.ContainsAny(decodedPath, "\x00\r\n\\") {
		return "", errors.New("contains an invalid escaped path")
	}
	for _, segment := range strings.Split(decodedPath, "/") {
		if segment == "." || segment == ".." {
			return "", errors.New("must not contain dot or parent segments")
		}
	}
	return value, nil
}
