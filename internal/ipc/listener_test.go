package ipc

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListenUnixCreatesPrivateSocketAndRejectsLiveListener(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "f.sock")
	listener, err := ListenUnix(path, 0o660)
	if isSocketPermissionError(err) {
		t.Skipf("execution sandbox does not permit Unix listeners under %s: %v", directory, err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o660 {
		t.Fatalf("socket mode = %#o, want 0660", info.Mode().Perm())
	}
	if _, err := ListenUnix(path, 0o660); err == nil {
		t.Fatal("second listener unexpectedly replaced a live socket")
	}
}

func TestListenUnixRecoversStaleSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.sock")
	old, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if isSocketPermissionError(err) {
		t.Skipf("execution sandbox does not permit Unix listeners: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	old.SetUnlinkOnClose(false)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	listener, err := ListenUnix(path, 0o660)
	if err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
}

func isSocketPermissionError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "operation not permitted") || strings.Contains(err.Error(), "permission denied"))
}

func TestListenUnixRefusesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontend.sock")
	if err := os.WriteFile(path, []byte("do not delete"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListenUnix(path, 0o660); err == nil {
		t.Fatal("regular file unexpectedly replaced")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "do not delete" {
		t.Fatalf("regular file changed: %q, %v", content, err)
	}
}

func TestListenLocalUsesLoopbackTCPWhenUnixIsDisabled(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	listener, err := ListenLocal(false, "", 0, port)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := listener.Addr().(*net.TCPAddr)
	if !address.IP.IsLoopback() || address.Port != port {
		t.Fatalf("listener address = %s, want loopback port %d", address, port)
	}
}
