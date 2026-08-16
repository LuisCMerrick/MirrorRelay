package upstreamnginx

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (c *Controller) discoverVersion(ctx context.Context) {
	out, err := c.runUpstreamNginx(ctx, "-V")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	versionLine := ""
	if len(lines) > 0 && lines[0] != "" {
		versionLine = lines[0]
	}
	architecture, checksum, binaryBuildID := upstreamNginxBinaryMetadata(c.cfg.UpstreamNginx.Binary, versionLine)
	if err != nil && out == "" && checksum == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if versionLine != "" {
		c.status.Version = versionLine
	}
	if len(lines) > 1 {
		c.status.BuildOptions = strings.Join(lines[1:], "\n")
	}
	c.status.Architecture = architecture
	c.status.SHA256 = checksum
	c.status.BuildID = binaryBuildID
}

func upstreamNginxBinaryMetadata(binary, versionLine string) (architecture, checksum, buildID string) {
	path, err := executablePath(binary)
	if err != nil {
		return "", "", ""
	}
	file, err := os.Open(path)
	if err != nil {
		return "", "", ""
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err == nil {
		checksum = fmt.Sprintf("%x", hash.Sum(nil))
	}
	_ = file.Close()

	if binaryELF, err := elf.Open(path); err == nil {
		switch binaryELF.Machine {
		case elf.EM_X86_64:
			architecture = "linux/amd64"
		case elf.EM_AARCH64:
			architecture = "linux/arm64"
		default:
			architecture = "linux/" + strings.ToLower(binaryELF.Machine.String())
		}
		_ = binaryELF.Close()
	}
	if checksum == "" {
		return architecture, "", ""
	}
	version := strings.TrimSpace(versionLine)
	if separator := strings.LastIndexByte(version, '/'); separator >= 0 {
		version = version[separator+1:]
	}
	if version == "" {
		version = "unknown"
	}
	archID := strings.ReplaceAll(architecture, "/", "-")
	if archID == "" {
		archID = "unknown"
	}
	buildID = fmt.Sprintf("nginx-%s-%s-%s", version, archID, checksum[:12])
	return architecture, checksum, buildID
}

func executablePath(binary string) (string, error) {
	resolved := binary
	if !strings.ContainsRune(binary, os.PathSeparator) {
		value, err := exec.LookPath(binary)
		if err != nil {
			return "", err
		}
		resolved = value
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	if evaluated, evaluateErr := filepath.EvalSymlinks(absolute); evaluateErr == nil {
		absolute = evaluated
	}
	return filepath.Clean(absolute), nil
}
