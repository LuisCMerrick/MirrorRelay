// Package config provides strict YAML configuration loading and validation.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load loads configuration from YAML file with environment variable overlays.
func Load(path string, dev bool) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if err == nil {
		decoder := yaml.NewDecoder(bytes.NewReader(b))
		decoder.KnownFields(true)
		if err := decoder.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("parse config: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("multiple YAML documents are not allowed")
			}
			return cfg, fmt.Errorf("parse config: %w", err)
		}
	} else if !dev {
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}
	if dev {
		applyDevDefaults(&cfg)
	}
	applyEnvironment(&cfg)
	if err := cfg.NormalizeRuntime(); err != nil {
		return cfg, fmt.Errorf("parse socket mode: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// DecodeImportReader strictly decodes and normalizes an imported YAML stream
// without running final semantic validation. The settings import workflow must
// first restore omitted instance-local bindings and redacted credentials, then
// call ApplyEnvironment before the candidate can be used.
func DecodeImportReader(r io.Reader) (Config, error) {
	cfg := Default()
	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple YAML documents are not allowed")
		}
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.NormalizeRuntime(); err != nil {
		return cfg, fmt.Errorf("parse socket mode: %w", err)
	}
	return cfg, nil
}

// LoadReader parses and validates a YAML stream into a Config.
func LoadReader(r io.Reader) (Config, error) {
	cfg, err := DecodeImportReader(r)
	if err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) FrontendEndpoint() (network, address string) {
	if c.Server.UnixSocketEnabled {
		return "unix", c.Server.FrontendSocket
	}
	connectAddress := c.Server.LocalAddress
	if ip := net.ParseIP(connectAddress); ip != nil && ip.IsUnspecified() {
		if ip.To4() == nil {
			connectAddress = "::1"
		} else {
			connectAddress = "127.0.0.1"
		}
	}
	return "tcp", net.JoinHostPort(connectAddress, strconv.Itoa(c.Server.LocalPort))
}

func (c Config) UpstreamEndpoint() (network, address string) {
	if c.UpstreamNginx.UpstreamSocketEnabled {
		return "unix", c.UpstreamNginx.UpstreamSocket
	}
	return "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(c.UpstreamNginx.UpstreamLocalPort))
}

func (c Config) AdminAPIPath() string {
	return c.Admin.Path + "api/v1/"
}

func (c *Config) NormalizeRuntime() error {
	adminPath, err := normalizeAdminPath(c.Admin.Path)
	if err != nil {
		return err
	}
	c.Admin.Path = adminPath
	c.Server.FrontendSocketMode, err = parseSocketMode(c.Server.FrontendSocketModeText)
	if err != nil {
		return err
	}
	c.UpstreamNginx.UpstreamSocketMode, err = parseSocketMode(c.UpstreamNginx.UpstreamSocketModeText)
	return err
}

func normalizeAdminPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || value == "/" || len(value) > 256 ||
		strings.Contains(value, "//") || strings.ContainsAny(value, "\\%?#\x00\r\n\t ") {
		return "", errors.New("admin.path must be an absolute URL path with safe segments")
	}
	segments := strings.Split(strings.Trim(value, "/"), "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || len(segment) > 64 {
			return "", errors.New("admin.path contains an invalid segment")
		}
		for _, character := range segment {
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') || strings.ContainsRune("._~-", character) {
				continue
			}
			return "", errors.New("admin.path contains an unsafe character")
		}
	}
	first := strings.ToLower(segments[0])
	if first == "healthz" || first == "metrics" || first == "_mirror_auth" || first == "_mirrorrelay" {
		return "", errors.New("admin.path conflicts with a reserved system path")
	}
	return "/" + strings.Join(segments, "/") + "/", nil
}

func EnsureDirectories(c Config) error {
	snippetDirectory := c.Ingress.SnippetPath
	if strings.EqualFold(filepath.Ext(snippetDirectory), ".conf") {
		snippetDirectory = filepath.Dir(snippetDirectory)
	}
	directories := []string{filepath.Dir(c.Database.Path), c.Cache.Path, c.Logging.Path, c.UpstreamNginx.Prefix, c.UpstreamNginx.LogPath, c.Runtime.Root, c.Runtime.RunDir, snippetDirectory, filepath.Dir(c.UpstreamNginx.PID)}
	if c.Ingress.Mode == "managed-standalone" {
		directories = append(directories, filepath.Dir(c.TLS.Certificate), filepath.Dir(c.TLS.PrivateKey))
	}
	if c.Server.UnixSocketEnabled {
		directories = append(directories, filepath.Dir(c.Server.FrontendSocket))
	}
	if c.UpstreamNginx.UpstreamSocketEnabled {
		directories = append(directories, filepath.Dir(c.UpstreamNginx.UpstreamSocket))
	}
	for _, dir := range directories {
		if dir == "." || strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	return nil
}
