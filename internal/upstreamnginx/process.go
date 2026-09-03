package upstreamnginx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type commandRunner interface {
	Run(context.Context, string, ...string) (string, error)
	Start(string, ...string) (processHandle, error)
}

type processHandle interface {
	PID() int
	Wait() error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

type execProcess struct {
	command *exec.Cmd
}

func (p execProcess) PID() int { return p.command.Process.Pid }
func (p execProcess) Wait() error {
	return p.command.Wait()
}

func (execRunner) Start(name string, args ...string) (processHandle, error) {
	command := exec.Command(name, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	return execProcess{command: command}, nil
}

func (c *Controller) activate(ctx context.Context) error {
	pid, running := c.currentPID()
	c.mu.Lock()
	if running {
		c.status.State = "reloading"
	} else {
		c.status.State = "starting"
	}
	c.mu.Unlock()
	if running {
		out, err := c.runUpstreamNginx(ctx, "-s", "reload", "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", filepath.Join(c.cfg.UpstreamNginx.Prefix, "current", "nginx.conf"))
		if err != nil {
			return fmt.Errorf("graceful reload: %w (%s)", err, strings.TrimSpace(out))
		}
		c.mu.Lock()
		c.status.State, c.status.PID = "running", pid
		c.mu.Unlock()
		return c.waitForUpstreamEndpoint(ctx)
	}
	if c.cfg.UpstreamNginx.Mode == "external" {
		return errors.New("externally managed Managed Upstream Nginx is not running; start it with the configured prefix before applying")
	}
	if err := c.checkPorts(); err != nil {
		return err
	}
	if err := c.prepareUpstreamEndpoint(); err != nil {
		return err
	}
	process, err := c.startUpstreamNginx("-e", filepath.Join(c.cfg.UpstreamNginx.LogPath, "bootstrap-error.log"), "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", filepath.Join(c.cfg.UpstreamNginx.Prefix, "current", "nginx.conf"), "-g", "daemon off;")
	if err != nil {
		return fmt.Errorf("start Managed Upstream Nginx: %w", err)
	}
	c.mu.Lock()
	c.childPID = process.PID()
	c.mu.Unlock()
	go c.waitForManagedProcess(process)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pid, ok := c.currentPID(); ok {
			if endpointErr := c.waitForUpstreamEndpoint(ctx); endpointErr != nil {
				return endpointErr
			}
			c.mu.Lock()
			c.status.State, c.status.PID, c.status.StartedAt = "running", pid, time.Now()
			c.mu.Unlock()
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("nginx did not create a live pid within 5 seconds")
}

func (c *Controller) Stop(ctx context.Context) error {
	if !c.Enabled() || c.cfg.UpstreamNginx.Mode != "managed" {
		return nil
	}
	if _, running := c.currentPID(); !running {
		return nil
	}
	c.mu.Lock()
	c.status.State = "stopping"
	c.mu.Unlock()
	out, err := c.runUpstreamNginx(ctx, "-s", "quit", "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", filepath.Join(c.cfg.UpstreamNginx.Prefix, "current", "nginx.conf"))
	if err != nil {
		return fmt.Errorf("graceful stop Managed Upstream Nginx: %w (%s)", err, strings.TrimSpace(out))
	}
	for {
		if _, running := c.currentPID(); !running {
			c.mu.Lock()
			c.status.State, c.status.PID = "stopped", 0
			c.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (c *Controller) runUpstreamNginx(ctx context.Context, args ...string) (string, error) {
	c.commandMu.Lock()
	defer c.commandMu.Unlock()
	return c.runner.Run(ctx, c.cfg.UpstreamNginx.Binary, args...)
}

func (c *Controller) startUpstreamNginx(args ...string) (processHandle, error) {
	c.commandMu.Lock()
	defer c.commandMu.Unlock()
	return c.runner.Start(c.cfg.UpstreamNginx.Binary, args...)
}

func (c *Controller) waitForManagedProcess(process processHandle) {
	err := process.Wait()
	exitCode := 0
	reason := "Managed Upstream Nginx exited"
	if err != nil {
		exitCode = -1
		reason = err.Error()
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				reason = "Managed Upstream Nginx terminated by signal " + status.Signal().String()
			} else {
				reason = fmt.Sprintf("Managed Upstream Nginx exited with code %d", exitCode)
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.LastExitAt = time.Now()
	c.status.LastExitCode = &exitCode
	c.status.LastExitReason = reason
	if c.childPID != process.PID() {
		return
	}
	c.childPID = 0
	c.status.PID = 0
	if c.status.State == "stopping" {
		c.status.State = "stopped"
	} else if c.status.State != "stopped" {
		c.status.State = "restarting"
	}
}

func (c *Controller) currentPID() (int, bool) {
	b, err := os.ReadFile(c.cfg.UpstreamNginx.PID)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return 0, false
	}
	actualInfo, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return 0, false
	}
	expected, err := executablePath(c.cfg.UpstreamNginx.Binary)
	if err != nil {
		return 0, false
	}
	expectedInfo, err := os.Stat(expected)
	if err != nil || !os.SameFile(actualInfo, expectedInfo) {
		return 0, false
	}
	return pid, true
}

func (c *Controller) supervise(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	backoff := c.cfg.UpstreamNginx.RestartInitialBackoff
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, ok := c.currentPID(); ok {
				backoff = c.cfg.UpstreamNginx.RestartInitialBackoff
				continue
			}
			c.mu.Lock()
			if c.status.State == "stopped" || c.status.State == "stopping" {
				c.mu.Unlock()
				continue
			}
			if c.childPID != 0 {
				c.mu.Unlock()
				continue
			}
			if c.status.State == "running" {
				unknownExitCode := -1
				c.status.LastExitAt = time.Now()
				c.status.LastExitCode = &unknownExitCode
				c.status.LastExitReason = "attached Managed Upstream Nginx process disappeared; exit status unavailable"
				c.status.State = "restarting"
				c.status.PID = 0
			}
			c.mu.Unlock()
			if !c.mayRestart() {
				c.setFailure(errors.New("nginx restart limit exceeded"))
				continue
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			c.applyMu.Lock()
			_, running := c.currentPID()
			var err error
			if !running {
				err = c.activate(ctx)
			}
			c.applyMu.Unlock()
			if err != nil {
				c.setFailure(err)
				backoff *= 2
				if backoff > c.cfg.UpstreamNginx.RestartMaxBackoff {
					backoff = c.cfg.UpstreamNginx.RestartMaxBackoff
				}
			}
		}
	}
}

func (c *Controller) mayRestart() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := time.Now().Add(-c.cfg.UpstreamNginx.RestartWindow)
	kept := c.failures[:0]
	for _, at := range c.failures {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	c.failures = kept
	if len(c.failures) >= c.cfg.UpstreamNginx.RestartMaxFailures {
		return false
	}
	c.failures = append(c.failures, time.Now())
	return true
}

func (c *Controller) setFailure(err error) {
	pid, running := c.currentPID()
	c.mu.Lock()
	defer c.mu.Unlock()
	if running {
		c.status.State, c.status.PID = "running", pid
	} else {
		c.status.State, c.status.PID = "failed", 0
	}
	c.status.LastError = err.Error()
	c.status.LastReload = time.Now()
	c.status.LastReloadResult = "failed"
}

func (c *Controller) checkPorts() error {
	addresses := make([]string, 0, 3)
	if c.cfg.Ingress.Mode == "managed-standalone" {
		addresses = append(addresses, c.cfg.HTTP.Listen, c.cfg.HTTP.HTTPSListen)
	}
	if !c.cfg.UpstreamNginx.UpstreamSocketEnabled {
		_, address := c.cfg.UpstreamEndpoint()
		addresses = append(addresses, address)
	}
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("listen port conflict on %s: %w", address, err)
		}
		_ = listener.Close()
	}
	return nil
}

func (c *Controller) prepareUpstreamEndpoint() error {
	if !c.cfg.UpstreamNginx.UpstreamSocketEnabled {
		return nil
	}
	info, err := os.Lstat(c.cfg.UpstreamNginx.UpstreamSocket)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect upstream socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket upstream path %s", c.cfg.UpstreamNginx.UpstreamSocket)
	}
	connection, dialErr := net.DialTimeout("unix", c.cfg.UpstreamNginx.UpstreamSocket, 250*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("upstream socket already has a live listener but Managed Upstream Nginx PID is unavailable")
	}
	if errors.Is(dialErr, os.ErrNotExist) {
		return nil
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("cannot determine whether upstream socket is stale: %w", dialErr)
	}
	if err := os.Remove(c.cfg.UpstreamNginx.UpstreamSocket); err != nil {
		return fmt.Errorf("remove stale upstream socket: %w", err)
	}
	return nil
}

func (c *Controller) waitForUpstreamEndpoint(ctx context.Context) error {
	network, address := c.cfg.UpstreamEndpoint()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout(network, address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			if network == "unix" {
				if chmodErr := os.Chmod(address, c.cfg.UpstreamNginx.UpstreamSocketMode); chmodErr != nil {
					return fmt.Errorf("set upstream socket permissions: %w", chmodErr)
				}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return fmt.Errorf("Managed Upstream Nginx did not create a reachable upstream %s endpoint %s within 5 seconds", network, address)
}
