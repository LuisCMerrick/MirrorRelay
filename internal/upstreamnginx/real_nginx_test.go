package upstreamnginx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LuisCMerrick/RepoGate/internal/config"
	"github.com/LuisCMerrick/RepoGate/internal/model"
)

// This test is opt-in because normal development environments are not
// required to have the Managed Upstream Nginx binary installed.
func TestRealManagedUpstreamNginxAcceptsGeneratedV5Configuration(t *testing.T) {
	binary := os.Getenv("REPOGATE_TEST_UPSTREAM_NGINX")
	if binary == "" {
		t.Skip("REPOGATE_TEST_UPSTREAM_NGINX is not set")
	}
	root := t.TempDir()
	runtimeDir, err := os.MkdirTemp(os.Getenv("TMPDIR"), "repogate-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	cfg := config.Default()
	cfg.UpstreamNginx.Binary = binary
	cfg.UpstreamNginx.Prefix = filepath.Join(root, "upstream-nginx")
	cfg.UpstreamNginx.PID = filepath.Join(runtimeDir, "upstream-nginx.pid")
	cfg.UpstreamNginx.LogPath = filepath.Join(root, "logs", "upstream-nginx")
	cfg.UpstreamNginx.UpstreamSocket = filepath.Join(runtimeDir, "upstream.sock")
	cfg.Server.FrontendSocket = filepath.Join(runtimeDir, "frontend.sock")
	cfg.Cache.Path = filepath.Join(root, "cache")
	cfg.UpstreamNginx.WorkerUser = "root"
	repository := model.Mirror{ID: 1, Name: "Repository", Slug: "repo", Type: "apt", Enabled: true,
		PublicMode: "path", PublicPath: "/repo/", ProxyMode: "transparent", CacheEnabled: true,
		Upstreams: []model.Upstream{{ID: 10, URL: "https://repo.example/base/", Host: "repo.example", Enabled: true}}}
	generated, err := NewGenerator(cfg, fixedResolver{"repo.example": {netip.MustParseAddr("8.8.8.8")}}).
		Generate(context.Background(), []model.Mirror{repository}, []model.CustomConfig{{
			Name: "upstream-tuning", Context: "upstream", RepositoryID: repository.ID, Enabled: true, Content: "keepalive_requests 1000;",
		}})
	if err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(cfg.UpstreamNginx.Prefix, "current")
	for _, directory := range []string{filepath.Join(current, "generated"), cfg.UpstreamNginx.LogPath,
		filepath.Join(cfg.UpstreamNginx.Prefix, "temp", "client"), filepath.Join(cfg.UpstreamNginx.Prefix, "temp", "proxy"),
		cfg.Cache.Path} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(current, "nginx.conf"), []byte(generated.Main), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "generated", "repositories.conf"), []byte(generated.Files["repositories.conf"]), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "generated", "upstreams.conf"), []byte(generated.Files["upstreams.conf"]), 0o640); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-t", "-p", cfg.UpstreamNginx.Prefix+string(os.PathSeparator), "-c", filepath.Join(current, "nginx.conf"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("nginx -t failed: %v\n%s", err, output)
	}
}

func TestRealManagedUpstreamNginxStreamsRepositoryResponse(t *testing.T) {
	binary := os.Getenv("REPOGATE_TEST_UPSTREAM_NGINX")
	if binary == "" {
		t.Skip("REPOGATE_TEST_UPSTREAM_NGINX is not set")
	}

	const repetitions = 512
	pattern := bytes.Repeat([]byte("RepoGate-stream-check-"), 1536)
	expectedHash := sha256.New()
	for range repetitions {
		_, _ = expectedHash.Write(pattern)
	}
	expectedSize := int64(len(pattern) * repetitions)
	auxiliaryBody := []byte("upstream-folder-icon")

	origin := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/icons/folder.gif" {
			response.Header().Set("Content-Type", "image/gif")
			_, _ = response.Write(auxiliaryBody)
			return
		}
		if request.URL.Path != "/base/large.bin" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/octet-stream")
		response.Header().Set("Content-Length", fmt.Sprintf("%d", expectedSize))
		for range repetitions {
			if _, err := response.Write(pattern); err != nil {
				return
			}
		}
	}))
	defer origin.Close()
	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, originPort, err := net.SplitHostPort(originURL.Host)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	runtimeDirectory, err := os.MkdirTemp(os.Getenv("TMPDIR"), "repogate-stream-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDirectory) })
	cfg := config.Default()
	cfg.UpstreamNginx.Binary = binary
	cfg.UpstreamNginx.Prefix = filepath.Join(root, "upstream-nginx")
	cfg.UpstreamNginx.PID = filepath.Join(runtimeDirectory, "upstream-nginx.pid")
	cfg.UpstreamNginx.LogPath = filepath.Join(root, "logs", "upstream-nginx")
	cfg.UpstreamNginx.UpstreamSocket = filepath.Join(runtimeDirectory, "upstream.sock")
	cfg.UpstreamNginx.CABundle = filepath.Join(root, "origin-ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: origin.Certificate().Raw})
	if err := os.WriteFile(cfg.UpstreamNginx.CABundle, certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	cfg.UpstreamNginx.WorkerUser = currentUser.Username
	cfg.Server.FrontendSocket = filepath.Join(runtimeDirectory, "frontend.sock")
	cfg.Cache.Path = filepath.Join(root, "cache")
	cfg.Security.AllowPrivateUpstream = true
	repository := model.Mirror{
		ID: 1, Name: "Large object", Slug: "large-object", Type: "generic", Enabled: true, HTMLRewriteEnabled: true,
		PublicMode: "path", PublicPath: "/large/", ProxyMode: "transparent",
		AllowPrivate: true,
		Upstreams: []model.Upstream{{
			ID: 1, URL: "https://example.com:" + originPort + "/base/", Host: "example.com:" + originPort, Enabled: true,
		}},
	}
	generated, err := NewGenerator(cfg, fixedResolver{"example.com": {netip.MustParseAddr("127.0.0.1")}}).
		Generate(context.Background(), []model.Mirror{repository}, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeRealNginxConfiguration(t, cfg, generated)

	command := exec.Command(binary, "-e", filepath.Join(cfg.UpstreamNginx.LogPath, "bootstrap-error.log"), "-p", cfg.UpstreamNginx.Prefix+string(os.PathSeparator), "-c", filepath.Join(cfg.UpstreamNginx.Prefix, "current", "nginx.conf"), "-g", "daemon off;")
	var processOutput bytes.Buffer
	command.Stdout = &processOutput
	command.Stderr = &processOutput
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- command.Wait() }()
	processExited := false
	t.Cleanup(func() {
		if processExited {
			return
		}
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-processDone:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-processDone
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		connection, dialErr := net.DialTimeout("unix", cfg.UpstreamNginx.UpstreamSocket, 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			break
		}
		select {
		case processErr := <-processDone:
			processExited = true
			t.Fatalf("Managed Upstream Nginx exited before creating its socket: %v\n%s", processErr, processOutput.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("Managed Upstream Nginx did not create its socket: %v\n%s", dialErr, processOutput.String())
		}
		time.Sleep(25 * time.Millisecond)
	}

	transport := &http.Transport{
		DisableCompression: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", cfg.UpstreamNginx.UpstreamSocket)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	request, err := http.NewRequest(http.MethodGet, "http://managed-upstream/_repo/1/1/package/large.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("stream through Managed Upstream Nginx: %v\n%s", err, processOutput.String())
	}
	defer response.Body.Close()
	actualHash := sha256.New()
	actualSize, err := io.Copy(actualHash, response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || actualSize != expectedSize || !bytes.Equal(actualHash.Sum(nil), expectedHash.Sum(nil)) {
		errorLog, _ := os.ReadFile(filepath.Join(cfg.UpstreamNginx.LogPath, "error.log"))
		t.Fatalf("unexpected streamed response: status=%d size=%d hash=%x\n%s\n%s", response.StatusCode, actualSize, actualHash.Sum(nil), errorLog, processOutput.String())
	}
	if strings.TrimSpace(response.Header.Get("X-Mirror-Cache")) != "" {
		t.Fatal("non-cache streaming smoke test unexpectedly reported a cache state")
	}
	_ = response.Body.Close()

	auxiliaryRequest, err := http.NewRequest(http.MethodGet, "http://managed-upstream/_repo_aux/1/1/metadata/icons/folder.gif", nil)
	if err != nil {
		t.Fatal(err)
	}
	auxiliaryResponse, err := client.Do(auxiliaryRequest)
	if err != nil {
		t.Fatalf("auxiliary resource through Managed Upstream Nginx: %v\n%s", err, processOutput.String())
	}
	defer auxiliaryResponse.Body.Close()
	actualAuxiliaryBody, err := io.ReadAll(auxiliaryResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if auxiliaryResponse.StatusCode != http.StatusOK || !bytes.Equal(actualAuxiliaryBody, auxiliaryBody) {
		t.Fatalf("unexpected auxiliary response: status=%d body=%q", auxiliaryResponse.StatusCode, actualAuxiliaryBody)
	}
}

func writeRealNginxConfiguration(t *testing.T, cfg config.Config, generated Generated) {
	t.Helper()
	current := filepath.Join(cfg.UpstreamNginx.Prefix, "current")
	for _, directory := range []string{
		filepath.Join(current, "generated"),
		cfg.UpstreamNginx.LogPath,
		filepath.Join(cfg.UpstreamNginx.Prefix, "run"),
		filepath.Join(cfg.UpstreamNginx.Prefix, "temp", "client"),
		filepath.Join(cfg.UpstreamNginx.Prefix, "temp", "proxy"),
		cfg.Cache.Path,
	} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"nginx.conf":                  generated.Main,
		"generated/repositories.conf": generated.Files["repositories.conf"],
		"generated/upstreams.conf":    generated.Files["upstreams.conf"],
	} {
		if err := os.WriteFile(filepath.Join(current, name), []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
}
