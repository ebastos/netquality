package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/ebastos/netquality/internal/baseline"
	"github.com/ebastos/netquality/internal/config"
	"github.com/ebastos/netquality/internal/eval"
	"github.com/ebastos/netquality/internal/probe"
	"github.com/ebastos/netquality/internal/store"
)

// Scheduler owns the main probe loop, rollup generation, retention pruning, and baseline recomputation.
type Scheduler struct {
	cfg    *config.Config
	db     *store.DB
	runner *probe.Runner
	engine *eval.Engine
}

// New wires the scheduler with its dependencies (runner for probes, engine for evaluation).
func New(cfg *config.Config, db *store.DB, runner *probe.Runner) *Scheduler {
	return &Scheduler{
		cfg:    cfg,
		db:     db,
		runner: runner,
		engine: eval.NewEngine(cfg, db),
	}
}

// Run starts the scheduler and blocks until the context is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	baseline.StartBackground(ctx, s.cfg, s.db)
	go s.rollupLoop(ctx)
	go s.retentionLoop(ctx)
	if s.cfg.PublicIP.Enabled {
		slog.Info("public ip tracking enabled", "endpoint", s.cfg.PublicIP.Endpoint, "interval", s.cfg.PublicIP.Interval.Std())
		go s.publicIPLoop(ctx)
	}

	interval := s.cfg.Schedule.Interval.Std()
	if s.cfg.Gateway.Enabled {
		slog.Info("gateway probe target", "host", s.runner.GatewayHost())
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// initial cycle
	s.runOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context) {
	samples, err := s.runner.RunCycle(ctx)
	if err != nil {
		slog.Error("probe cycle", "err", err)
	}
	if len(samples) > 0 {
		if err := s.db.InsertSamples(ctx, samples); err != nil {
			slog.Error("insert samples", "err", err)
		}
	}

	window := max(int64(s.cfg.State.Debounce.Std().Seconds())+60, 120)
	since := store.NowUnix() - window
	byProbe, err := s.db.RecentSamplesByProbe(ctx, since)
	if err != nil {
		slog.Error("recent samples", "err", err)
		return
	}
	if err := s.engine.Evaluate(ctx, byProbe); err != nil {
		slog.Error("evaluate", "err", err)
	}
}

func (s *Scheduler) rollupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	const bucketSec = 300

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := store.NowUnix()
			from := now - bucketSec*2
			samples, err := s.db.SamplesRange(ctx, "", from, now)
			if err != nil {
				slog.Error("rollup samples", "err", err)
				continue
			}
			for _, r := range store.BuildRollupsFromSamples(samples, bucketSec) {
				if err := s.db.UpsertRollup(ctx, r); err != nil {
					slog.Error("upsert rollup", "err", err)
				}
			}
		}
	}
}

func (s *Scheduler) retentionLoop(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := store.NowUnix()
			rawBefore := now - int64(s.cfg.Retention.RawDays)*86400
			rollupBefore := now - int64(s.cfg.Retention.RollupDays)*86400
			if err := s.db.PruneRaw(ctx, rawBefore); err != nil {
				slog.Error("prune raw", "err", err)
			}
			if err := s.db.PruneRollups(ctx, rollupBefore); err != nil {
				slog.Error("prune rollups", "err", err)
			}
		}
	}
}

func (s *Scheduler) publicIPLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PublicIP.Interval.Std())
	defer ticker.Stop()

	// initial check
	s.checkPublicIP(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkPublicIP(ctx)
		}
	}
}

func (s *Scheduler) checkPublicIP(ctx context.Context) {
	res, err := probe.PublicIP(ctx, s.cfg.PublicIP.Endpoint, 10*time.Second)
	if err != nil {
		slog.Warn("public ip check failed", "err", err)
		return
	}
	ts := store.NowUnix()
	if err := s.db.RecordPublicIPObservation(ctx, ts, res.IP, res.CGNAT); err != nil {
		slog.Error("record public ip", "err", err)
	}
}
