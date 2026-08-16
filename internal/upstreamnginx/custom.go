package upstreamnginx

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

var forbiddenDirectives = []string{
	"user", "pid", "daemon", "master_process", "load_module", "env", "working_directory",
	"root", "alias", "perl", "exec", "include", "server", "location", "upstream", "listen",
	"set", "rewrite", "return", "try_files", "error_page", "recursive_error_pages", "auth_request", "mirror",
	"proxy_pass", "grpc_pass",
	"fastcgi_pass", "uwsgi_pass", "scgi_pass", "memcached_pass", "ssl_certificate",
	"ssl_certificate_key", "ssl_session_ticket_key", "auth_basic_user_file", "access_log",
	"error_log", "client_body_temp_path", "proxy_temp_path", "fastcgi_temp_path",
	"uwsgi_temp_path", "scgi_temp_path", "proxy_store", "proxy_store_access", "proxy_cache_path",
	"proxy_cache", "proxy_cache_key", "proxy_cache_bypass", "proxy_no_cache", "proxy_ignore_headers",
}

var forbiddenDirectivePrefixes = []string{"proxy_ssl_"}

var reservedCustomTokens = []string{"$repo_", "$health_", "$http_x_mirror_internal_", "x-mirror-internal-", "mirrorrelay_repo_", "mirrorrelay_cache", "mirrorrelay_frontend"}

type customFragments struct {
	http                 string
	server               string
	byRepository         map[int64]string
	byUpstreamRepository map[int64]string
}

func classifyCustom(values []model.CustomConfig) (customFragments, error) {
	result := customFragments{byRepository: make(map[int64]string), byUpstreamRepository: make(map[int64]string)}
	for _, value := range values {
		if !value.Enabled {
			continue
		}
		if err := ValidateCustomName(value.Name); err != nil {
			return result, fmt.Errorf("custom configuration name: %w", err)
		}
		if err := ValidateCustom(value.Context, value.Content); err != nil {
			return result, fmt.Errorf("custom %s: %w", value.Name, err)
		}
		fragment := "        # BEGIN CUSTOM: " + value.Name + "\n" + indent(value.Content, "        ") + "\n        # END CUSTOM: " + value.Name + "\n"
		switch value.Context {
		case "http":
			if value.RepositoryID != 0 {
				return result, fmt.Errorf("custom %s: http context must be global", value.Name)
			}
			result.http += fragment
		case "upstream":
			if value.RepositoryID <= 0 {
				return result, fmt.Errorf("custom %s: upstream context requires repository_id", value.Name)
			}
			result.byUpstreamRepository[value.RepositoryID] += fragment
		case "server":
			if value.RepositoryID != 0 {
				return result, fmt.Errorf("custom %s: server context must be global", value.Name)
			}
			result.server += fragment
		case "location", "repository":
			if value.RepositoryID <= 0 {
				return result, fmt.Errorf("custom %s: %s context requires repository_id", value.Name, value.Context)
			}
			result.byRepository[value.RepositoryID] += fragment
		default:
			return result, fmt.Errorf("custom %s: unsupported context %s", value.Name, value.Context)
		}
	}
	return result, nil
}

func ValidateCustomName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return errors.New("name must contain 1..100 characters")
	}
	if strings.ContainsAny(name, "\x00\r\n") {
		return errors.New("name contains control characters")
	}
	return nil
}

func ValidateCustom(contextName, content string) error {
	contextName = strings.ToLower(strings.TrimSpace(contextName))
	if contextName != "http" && contextName != "server" && contextName != "location" && contextName != "upstream" && contextName != "repository" {
		return errors.New("context must be http, server, location, upstream or repository")
	}
	if len(content) > 1<<20 {
		return errors.New("custom configuration exceeds 1 MiB")
	}
	if strings.Contains(content, "\x00") {
		return errors.New("custom configuration contains NUL")
	}
	depth, directiveStart := 0, true
	var token strings.Builder
	var quote rune
	escaped, comment, variableBrace := false, false, false
	flush := func() error {
		if token.Len() == 0 {
			return nil
		}
		value := strings.ToLower(token.String())
		normalizedValue := strings.NewReplacer("{", "", "}", "").Replace(value)
		token.Reset()
		for _, reserved := range reservedCustomTokens {
			if strings.Contains(normalizedValue, reserved) {
				return fmt.Errorf("token containing %s is reserved for generated configuration", reserved)
			}
		}
		if directiveStart {
			for _, directive := range forbiddenDirectives {
				if value == directive {
					return fmt.Errorf("directive %s is not allowed", directive)
				}
			}
			for _, prefix := range forbiddenDirectivePrefixes {
				if strings.HasPrefix(value, prefix) {
					return fmt.Errorf("directive prefix %s is reserved for generated configuration", prefix)
				}
			}
			directiveStart = false
		}
		return nil
	}
	for _, character := range content {
		if comment {
			if character == '\n' {
				comment = false
			}
			continue
		}
		if quote != 0 {
			if escaped {
				token.WriteRune(character)
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				quote = 0
				continue
			}
			token.WriteRune(character)
			continue
		}
		switch character {
		case '#':
			if err := flush(); err != nil {
				return err
			}
			comment = true
		case '\'', '"':
			quote = character
		case '\\':
			return errors.New("backslash escapes are not allowed in custom configuration outside quoted values")
		case '{':
			if token.Len() > 0 && strings.HasSuffix(token.String(), "$") {
				token.WriteRune(character)
				variableBrace = true
				continue
			}
			if err := flush(); err != nil {
				return err
			}
			depth++
			directiveStart = true
		case '}':
			if variableBrace {
				token.WriteRune(character)
				variableBrace = false
				continue
			}
			if err := flush(); err != nil {
				return err
			}
			depth--
			if depth < 0 {
				return errors.New("custom configuration escapes its context")
			}
			directiveStart = true
		case ';':
			if err := flush(); err != nil {
				return err
			}
			directiveStart = true
		case ' ', '\t', '\r', '\n':
			if err := flush(); err != nil {
				return err
			}
		default:
			token.WriteRune(character)
		}
	}
	if quote != 0 || escaped || variableBrace {
		return errors.New("unterminated quoted string in custom configuration")
	}
	if err := flush(); err != nil {
		return err
	}
	if depth != 0 {
		return errors.New("unbalanced braces in custom configuration")
	}
	return nil
}
