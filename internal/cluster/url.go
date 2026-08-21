package cluster

import (
	"context"
	"fmt"
	"net"

	"github.com/LuisCMerrick/MirrorRelay/internal/config"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

const (
	SyncApplyPath = "/api/v1/cluster/sync/apply"
	SyncPurgePath = "/api/v1/cluster/sync/purge"
)

// ValidateNodeURL applies the complete cluster-node policy, including the
// cluster-specific HTTP opt-in and the global private-address policy.
func ValidateNodeURL(ctx context.Context, cfg config.Config, rawURL string) (string, error) {
	origin, err := security.ParseOriginURL(rawURL, cfg.Distributed.AllowHTTP)
	if err != nil {
		return "", err
	}
	canonical := origin.String()
	if err := security.ValidateResolvedURL(ctx, canonical, cfg.Distributed.AllowHTTP,
		cfg.Security.AllowPrivateUpstream, net.DefaultResolver); err != nil {
		return "", fmt.Errorf("validate resolved cluster node URL: %w", err)
	}
	return canonical, nil
}

func nodeEndpointURL(ctx context.Context, cfg config.Config, rawURL, endpoint string) (string, error) {
	canonical, err := ValidateNodeURL(ctx, cfg, rawURL)
	if err != nil {
		return "", err
	}
	target, err := security.ResolveOriginEndpoint(canonical, endpoint, cfg.Distributed.AllowHTTP)
	if err != nil {
		return "", err
	}
	if err := security.ValidateResolvedURL(ctx, target, cfg.Distributed.AllowHTTP,
		cfg.Security.AllowPrivateUpstream, net.DefaultResolver); err != nil {
		return "", fmt.Errorf("validate cluster endpoint: %w", err)
	}
	return target, nil
}
