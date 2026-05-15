package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ebastos/netquality/internal/config"
	"github.com/ebastos/netquality/internal/eval"
	"github.com/ebastos/netquality/internal/store"
)

func TestStatusIncludesLearningWhenNotWarm(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.Config{
		DeviceID: "test-pi",
		Baseline: config.BaselineConfig{WarmupDays: 14},
	}
	engine := eval.NewEngine(cfg, db)
	srv := New(cfg, db, engine)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["warm"] != false {
		t.Fatalf("expected warm false, got %v", body["warm"])
	}
	learning, ok := body["learning"].(map[string]any)
	if !ok {
		t.Fatalf("expected learning object, got %v", body["learning"])
	}
	for _, key := range []string{
		"warmup_days", "first_sample_ts", "days_collected",
		"time_progress", "baselines_ready", "baseline_row_count",
	} {
		if _, exists := learning[key]; !exists {
			t.Fatalf("learning missing key %q", key)
		}
	}
	if learning["warmup_days"] != float64(14) {
		t.Fatalf("warmup_days %v", learning["warmup_days"])
	}
}

func TestStatusStatesUseSnakeCase(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := t.Context()
	if err := db.SetState(ctx, "overall", "ok", store.NowUnix(), nil); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{DeviceID: "test-pi", Baseline: config.BaselineConfig{WarmupDays: 14}}
	srv := New(cfg, db, eval.NewEngine(cfg, db))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	var body struct {
		States []map[string]any `json:"states"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.States) != 1 {
		t.Fatalf("states len %d", len(body.States))
	}
	st := body.States[0]
	if st["dimension"] != "overall" || st["state"] != "ok" {
		t.Fatalf("unexpected state map: %v", st)
	}
	if _, ok := st["since_ts"]; !ok {
		t.Fatalf("missing since_ts: %v", st)
	}
}
