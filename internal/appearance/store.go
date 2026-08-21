// Package appearance publishes immutable runtime appearance snapshots.
package appearance

import (
	"sync/atomic"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

type Store struct {
	value atomic.Value
}

func New(initial model.UIEnhancementConfig) *Store {
	store := &Store{}
	store.value.Store(initial)
	return store
}

func (s *Store) Load() model.UIEnhancementConfig {
	return s.value.Load().(model.UIEnhancementConfig)
}

func (s *Store) Store(value model.UIEnhancementConfig) {
	s.value.Store(value)
}
