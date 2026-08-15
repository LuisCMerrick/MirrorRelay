package proxy

import (
	"errors"
	"fmt"
	"strings"
)

type authParameter struct {
	name  string
	value string
}

type bearerChallenge struct {
	parameters []authParameter
}

func parseBearerChallenge(value string) (bearerChallenge, error) {
	value = strings.TrimSpace(value)
	if len(value) < len("Bearer") || !strings.EqualFold(value[:len("Bearer")], "Bearer") {
		return bearerChallenge{}, errors.New("not a Bearer challenge")
	}
	if len(value) > len("Bearer") && value[len("Bearer")] != ' ' && value[len("Bearer")] != '\t' {
		return bearerChallenge{}, errors.New("invalid Bearer scheme delimiter")
	}
	rest := strings.TrimSpace(value[len("Bearer"):])
	if rest == "" {
		return bearerChallenge{}, errors.New("Bearer challenge has no parameters")
	}
	segments, err := splitAuthParameters(rest)
	if err != nil {
		return bearerChallenge{}, err
	}
	challenge := bearerChallenge{parameters: make([]authParameter, 0, len(segments))}
	for _, segment := range segments {
		name, raw, ok := strings.Cut(segment, "=")
		name = strings.TrimSpace(name)
		raw = strings.TrimSpace(raw)
		if !ok || !isToken(name) || raw == "" {
			return bearerChallenge{}, fmt.Errorf("invalid Bearer auth parameter %q", segment)
		}
		decoded, err := decodeAuthValue(raw)
		if err != nil {
			return bearerChallenge{}, fmt.Errorf("invalid Bearer parameter %s: %w", name, err)
		}
		challenge.parameters = append(challenge.parameters, authParameter{name: name, value: decoded})
	}
	return challenge, nil
}

func splitAuthParameters(value string) ([]string, error) {
	var result []string
	start := 0
	quoted := false
	escaped := false
	for index, char := range value {
		if escaped {
			escaped = false
			continue
		}
		if quoted && char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			quoted = !quoted
			continue
		}
		if char == ',' && !quoted {
			part := strings.TrimSpace(value[start:index])
			if part == "" {
				return nil, errors.New("empty Bearer auth parameter")
			}
			result = append(result, part)
			start = index + 1
		}
	}
	if quoted || escaped {
		return nil, errors.New("unterminated quoted Bearer auth parameter")
	}
	part := strings.TrimSpace(value[start:])
	if part == "" {
		return nil, errors.New("empty Bearer auth parameter")
	}
	return append(result, part), nil
}

func decodeAuthValue(value string) (string, error) {
	if !strings.HasPrefix(value, "\"") {
		if !isToken(value) {
			return "", errors.New("unquoted value is not a token")
		}
		return value, nil
	}
	if len(value) < 2 || value[len(value)-1] != '"' {
		return "", errors.New("unterminated quoted string")
	}
	var result strings.Builder
	escaped := false
	for _, char := range value[1 : len(value)-1] {
		if escaped {
			result.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '"' || char == '\r' || char == '\n' {
			return "", errors.New("invalid quoted string character")
		}
		result.WriteRune(char)
	}
	if escaped {
		return "", errors.New("unterminated escape")
	}
	return result.String(), nil
}

func (c bearerChallenge) get(name string) (string, bool) {
	for _, parameter := range c.parameters {
		if strings.EqualFold(parameter.name, name) {
			return parameter.value, true
		}
	}
	return "", false
}

func (c *bearerChallenge) set(name, value string) {
	for index := range c.parameters {
		if strings.EqualFold(c.parameters[index].name, name) {
			c.parameters[index].value = value
			return
		}
	}
	c.parameters = append(c.parameters, authParameter{name: name, value: value})
}

func (c bearerChallenge) String() string {
	parts := make([]string, 0, len(c.parameters))
	for _, parameter := range c.parameters {
		value := strings.ReplaceAll(parameter.value, "\\", "\\\\")
		value = strings.ReplaceAll(value, "\"", "\\\"")
		parts = append(parts, parameter.name+"=\""+value+"\"")
	}
	return "Bearer " + strings.Join(parts, ",")
}

func isToken(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			continue
		}
		switch char {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~', ':', '/':
			continue
		default:
			return false
		}
	}
	return true
}
