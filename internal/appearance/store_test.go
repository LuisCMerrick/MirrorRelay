package appearance

import (
	"fmt"
	"sync"
	"testing"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestStoreConcurrentSnapshots(t *testing.T) {
	store := New(model.UIEnhancementConfig{Theme: "system", AccentColor: "#2563eb"})
	var wait sync.WaitGroup
	for reader := 0; reader < 8; reader++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 2000; iteration++ {
				snapshot := store.Load()
				if snapshot.Theme == "" || snapshot.AccentColor == "" {
					t.Errorf("observed a partial appearance snapshot: %+v", snapshot)
					return
				}
			}
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		for iteration := 0; iteration < 2000; iteration++ {
			store.Store(model.UIEnhancementConfig{
				Enabled:     iteration%2 == 0,
				Theme:       "dark",
				AccentColor: fmt.Sprintf("#%06x", iteration),
			})
		}
	}()
	wait.Wait()
}
