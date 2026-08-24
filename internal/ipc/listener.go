// Package ipc provides inter-process communication utilities.
package ipc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ListenLocal selects the private Unix listener or a configured IP TCP
// endpoint. Wildcard binding is accepted only when explicitly configured.
func ListenLocal(unixEnabled bool, path string, mode os.FileMode, localAddress string, port int) (net.Listener, error) {
	if unixEnabled {
		return ListenUnix(path, mode)
	}
	if localAddress == "" || strings.TrimSpace(localAddress) != localAddress {
		return nil, errors.New("local TCP address must be a valid IP listen address")
	}
	ip := net.ParseIP(localAddress)
	if ip == nil || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.Equal(net.IPv4bcast) {
		return nil, errors.New("local TCP address must be a valid IP listen address")
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("local TCP port must be 1..65535")
	}
	address := net.JoinHostPort(localAddress, strconv.Itoa(port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on local TCP endpoint %s: %w", address, err)
	}
	return listener, nil
}

// ListenUnix creates a private Unix listener after safely recovering a stale
// socket. A live listener, a non-socket path, or an indeterminate permission
// error is never unlinked.
func ListenUnix(path string, mode os.FileMode) (*net.UnixListener, error) {
	if path == "" {
		return nil, errors.New("Unix socket path is empty")
	}
	if mode != 0o660 {
		return nil, fmt.Errorf("Unix socket mode must be 0660, got %#o", mode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create Unix socket directory: %w", err)
	}
	if err := recoverStale(path); err != nil {
		return nil, err
	}
	address := &net.UnixAddr{Name: path, Net: "unix"}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return nil, fmt.Errorf("listen on Unix socket %s: %w", path, err)
	}
	listener.SetUnlinkOnClose(true)
	if err := os.Chmod(path, mode); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("set Unix socket permissions: %w", err)
	}
	return listener, nil
}

func recoverStale(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Unix socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket path %s", path)
	}
	conn, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("Unix socket %s already has a live listener", path)
	}
	if errors.Is(dialErr, os.ErrNotExist) {
		return nil
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		if errors.Is(dialErr, syscall.EACCES) || errors.Is(dialErr, syscall.EPERM) {
			return fmt.Errorf("cannot inspect Unix socket permissions: %w", dialErr)
		}
		return fmt.Errorf("cannot determine whether Unix socket is stale: %w", dialErr)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale Unix socket: %w", err)
	}
	return nil
}
