package eval

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebastos/netquality/internal/config"
	"github.com/ebastos/netquality/internal/store"
)

func TestStateMachineDebounce(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "deploy", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.State.Debounce = config.Duration(1 * time.Second)

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sm := NewStateMachine(cfg)
	ctx := context.Background()
	now := int64(1000)

	cur, err := sm.Apply(ctx, db, "gateway", StateDegraded, now)
	if err != nil || cur != StateOK {
		t.Fatalf("first apply cur=%s err=%v", cur, err)
	}
	cur, err = sm.Apply(ctx, db, "gateway", StateDegraded, now+2)
	if err != nil || cur != StateDegraded {
		t.Fatalf("after debounce cur=%s err=%v", cur, err)
	}
}
