package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

type Info struct {
	Version        string `json:"version"`
	GitCommit      string `json:"git_commit"`
	BuildTimestamp string `json:"build_timestamp"`
	BuildID        string `json:"build_id"`
	GoVersion      string `json:"go_version"`
	TargetOS       string `json:"target_os"`
	Architecture   string `json:"architecture"`
}

func New(version, gitCommit, buildTimestamp, buildID string) Info {
	if strings.TrimSpace(version) == "" {
		version = "0.0.1"
	}
	if strings.TrimSpace(gitCommit) == "" {
		gitCommit = "unknown"
	}
	if strings.TrimSpace(buildTimestamp) == "" {
		buildTimestamp = "unknown"
	}
	if strings.TrimSpace(buildID) == "" {
		buildID = version
		if gitCommit != "unknown" {
			shortCommit := gitCommit
			if len(shortCommit) > 12 {
				shortCommit = shortCommit[:12]
			}
			buildID += "-" + shortCommit
		}
	}
	return Info{
		Version:        version,
		GitCommit:      gitCommit,
		BuildTimestamp: buildTimestamp,
		BuildID:        buildID,
		GoVersion:      runtime.Version(),
		TargetOS:       runtime.GOOS,
		Architecture:   runtime.GOARCH,
	}
}

func (i Info) Short() string {
	return fmt.Sprintf("repogate %s (build %s)", i.Version, i.BuildID)
}

func (i Info) Verbose() string {
	return fmt.Sprintf(`RepoGate Version: %s
Git Commit: %s
Build Timestamp: %s
Go Version: %s
Target OS: %s
Target Architecture: %s
Build ID: %s`, i.Version, i.GitCommit, i.BuildTimestamp, i.GoVersion, i.TargetOS, i.Architecture, i.BuildID)
}
