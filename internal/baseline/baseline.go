package baseline

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/ebastos/netquality/internal/config"
	"github.com/ebastos/netquality/internal/store"
)

type Recomputer struct {
	cfg *config.Config
	db  *store.DB
}

func NewRecomputer(cfg *config.Config, db *store.DB) *Recomputer {
	return &Recomputer{cfg: cfg, db: db}
}

func (r *Recomputer) Run(ctx context.Context) error {
	window := int64(r.cfg.Baseline.WarmupDays) * 86400
	since := store.NowUnix() - window
	samples, err := r.db.BaselineSamples(ctx, since)
	if err != nil {
		return err
	}
	if len(samples) == 0 {
		return nil
	}

	type key struct {
		probe      string
		metric     string
		hourOfWeek int
	}
	groups := make(map[key][]float64)
	for _, s := range samples {
		if s.Metric != "latency_ms" && s.Metric != "loss_pct" && s.Metric != "jitter_ms" {
			continue
		}
		h := store.HourOfWeek(time.Unix(s.Ts, 0))
		k := key{probe: s.Probe, metric: s.Metric, hourOfWeek: h}
		groups[k] = append(groups[k], s.Value)
	}

	now := store.NowUnix()
	for k, vals := range groups {
		if len(vals) < 5 {
			continue
		}
		sort.Float64s(vals)
		p50 := store.Percentile(vals, 0.5)
		p95 := store.Percentile(vals, 0.95)
		if err := r.db.UpsertBaseline(ctx, store.Baseline{
			Probe:      k.probe,
			Metric:     k.metric,
			HourOfWeek: k.hourOfWeek,
			P50:        p50,
			P95:        p95,
			UpdatedAt:  now,
		}); err != nil {
			return err
		}
	}
	slog.Info("baselines recomputed", "groups", len(groups))
	return nil
}

func StartBackground(ctx context.Context, cfg *config.Config, db *store.DB) {
	rec := NewRecomputer(cfg, db)
	interval := cfg.Baseline.RecomputeInterval.Std()
	go func() {
		_ = rec.Run(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := rec.Run(ctx); err != nil {
					slog.Error("baseline recompute", "err", err)
				}
			}
		}
	}()
}
