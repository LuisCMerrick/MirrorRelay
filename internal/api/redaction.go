package api

import (
	"net/url"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

const redactedValue = "[REDACTED]"

func mirrorsForRole(values []model.Mirror, role string) []model.Mirror {
	if role != "viewer" {
		return values
	}
	redacted := make([]model.Mirror, len(values))
	for index, value := range values {
		redacted[index] = mirrorForRole(value, role)
	}
	return redacted
}

func mirrorForRole(value model.Mirror, role string) model.Mirror {
	if role != "viewer" {
		return value
	}
	if value.HeaderAdd != nil {
		headers := make(map[string]string, len(value.HeaderAdd))
		for name := range value.HeaderAdd {
			headers[name] = redactedValue
		}
		value.HeaderAdd = headers
	}
	value.Upstreams = append([]model.Upstream(nil), value.Upstreams...)
	for index := range value.Upstreams {
		value.Upstreams[index].URL = redactURLQuery(value.Upstreams[index].URL)
	}
	value.TokenUpstream = redactURLQuery(value.TokenUpstream)
	return value
}

func redactURLQuery(value string) string {
	parsed, err := url.Parse(value)
	if err == nil && parsed.RawQuery != "" {
		parsed.RawQuery = "redacted=" + url.QueryEscape(redactedValue)
		return parsed.String()
	}
	return value
}
