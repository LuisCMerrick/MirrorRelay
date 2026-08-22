package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

const (
	CoordinatorEpochSettingKey = "coordinator_epoch_v2"
	EdgeSyncStateSettingKey    = "edge_sync_state_v2"
)

type SettingStore interface {
	ClusterSetting(context.Context, string) (string, bool, error)
	PutClusterSetting(context.Context, string, string) error
}

func EnsureCoordinatorEpoch(ctx context.Context, store SettingStore) (string, error) {
	if store == nil {
		return "", errors.New("cluster setting store is unavailable")
	}
	if value, found, err := store.ClusterSetting(ctx, CoordinatorEpochSettingKey); err != nil {
		return "", fmt.Errorf("read coordinator epoch: %w", err)
	} else if found {
		value = strings.TrimSpace(value)
		if !ValidCoordinatorEpoch(value) {
			return "", errors.New("stored coordinator epoch is invalid")
		}
		return value, nil
	}

	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate coordinator epoch: %w", err)
	}
	epoch := hex.EncodeToString(random[:])
	if err := store.PutClusterSetting(ctx, CoordinatorEpochSettingKey, epoch); err != nil {
		return "", fmt.Errorf("persist coordinator epoch: %w", err)
	}
	return epoch, nil
}

func ValidCoordinatorEpoch(value string) bool {
	if len(value) != 32 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 16
}

func LoadEdgeSyncState(ctx context.Context, store SettingStore) (model.ClusterSyncState, bool, error) {
	if store == nil {
		return model.ClusterSyncState{}, false, nil
	}
	raw, found, err := store.ClusterSetting(ctx, EdgeSyncStateSettingKey)
	if err != nil || !found {
		return model.ClusterSyncState{}, found, err
	}
	var state model.ClusterSyncState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return model.ClusterSyncState{}, false, fmt.Errorf("decode accepted cluster sync state: %w", err)
	}
	if err := validateEdgeSyncState(state); err != nil {
		return model.ClusterSyncState{}, false, err
	}
	return state, true, nil
}

func PutEdgeSyncState(ctx context.Context, store SettingStore, state model.ClusterSyncState) error {
	if store == nil {
		return errors.New("cluster setting store is unavailable")
	}
	if err := validateEdgeSyncState(state); err != nil {
		return err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode accepted cluster sync state: %w", err)
	}
	if err := store.PutClusterSetting(ctx, EdgeSyncStateSettingKey, string(encoded)); err != nil {
		return fmt.Errorf("persist accepted cluster sync state: %w", err)
	}
	return nil
}

func validateEdgeSyncState(state model.ClusterSyncState) error {
	if strings.TrimSpace(state.CoordinatorID) == "" || state.CoordinatorID != strings.TrimSpace(state.CoordinatorID) ||
		!ValidCoordinatorEpoch(state.CoordinatorEpoch) || state.ConfigGeneration <= 0 ||
		strings.TrimSpace(state.ConfigFingerprint) == "" || (state.Status != "pending" && state.Status != "applied") {
		return errors.New("accepted cluster sync state is invalid")
	}
	seen := make(map[string]struct{}, len(state.RetiredEpochs))
	for _, epoch := range state.RetiredEpochs {
		if !ValidCoordinatorEpoch(epoch) || epoch == state.CoordinatorEpoch {
			return errors.New("accepted cluster sync state contains an invalid retired epoch")
		}
		if _, exists := seen[epoch]; exists {
			return errors.New("accepted cluster sync state contains a duplicate retired epoch")
		}
		seen[epoch] = struct{}{}
	}
	return nil
}
