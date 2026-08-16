package upstreamnginx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (c *Controller) rotateUpstreamNginxLogs(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	pendingReopen := false
	run := func() {
		rotated, err := rotateUpstreamNginxLogFiles(c.cfg.UpstreamNginx.LogPath, time.Now(), c.cfg.Logging.MaxSizeMB<<20, time.Duration(c.cfg.Logging.KeepDays)*24*time.Hour)
		if err != nil {
			slog.Warn("rotate Managed Upstream Nginx logs", "error", err)
			return
		}
		pendingReopen = pendingReopen || rotated
		if !pendingReopen {
			return
		}
		if _, running := c.currentPID(); !running {
			return
		}
		reopenCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, reopenErr := c.runUpstreamNginx(reopenCtx, "-s", "reopen", "-p", withTrailingSlash(c.cfg.UpstreamNginx.Prefix), "-c", filepath.Join(c.cfg.UpstreamNginx.Prefix, "current", "nginx.conf"))
		cancel()
		if reopenErr != nil {
			slog.Warn("reopen Managed Upstream Nginx logs", "error", reopenErr, "output", strings.TrimSpace(out))
			return
		}
		pendingReopen = false
	}
	run()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func rotateUpstreamNginxLogFiles(directory string, now time.Time, maximumSize int64, keep time.Duration) (bool, error) {
	if maximumSize <= 0 {
		maximumSize = 1024 << 20
	}
	if keep <= 0 {
		keep = 30 * 24 * time.Hour
	}
	rotated := false
	for _, name := range []string{"access", "error"} {
		path := filepath.Join(directory, name+".log")
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return rotated, err
		}
		if info.Size() < maximumSize && info.ModTime().Format("2006-01-02") == now.Format("2006-01-02") {
			continue
		}
		target, err := nextRotatedLogPath(directory, name, info.ModTime())
		if err != nil {
			return rotated, err
		}
		if err := os.Rename(path, target); err != nil {
			return rotated, err
		}
		rotated = true
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return rotated, nil
	}
	if err != nil {
		return rotated, err
	}
	cutoff := now.Add(-keep)
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasPrefix(entry.Name(), "access-") && !strings.HasPrefix(entry.Name(), "error-")) || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil && info.ModTime().Before(cutoff) {
			if removeErr := os.Remove(filepath.Join(directory, entry.Name())); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return rotated, removeErr
			}
		}
	}
	return rotated, nil
}

func nextRotatedLogPath(directory, name string, modified time.Time) (string, error) {
	base := name + "-" + modified.Format("2006-01-02")
	for sequence := 0; sequence < 1_000_000; sequence++ {
		fileName := base + ".log"
		if sequence > 0 {
			fileName = base + "." + strconv.Itoa(sequence) + ".log"
		}
		path := filepath.Join(directory, fileName)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("Managed Upstream Nginx log rotation sequence exhausted")
}

func (c *Controller) refreshResolvedUpstreams(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.UpstreamNginx.ResolverRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := c.Reconcile(ctx, "system", "scheduled safe DNS refresh"); err != nil {
				c.setFailure(fmt.Errorf("safe DNS refresh: %w", err))
			}
		}
	}
}
