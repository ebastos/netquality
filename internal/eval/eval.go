package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ebastos/netquality/internal/config"
	"github.com/ebastos/netquality/internal/store"
)

type Engine struct {
	cfg    *config.Config
	db     *store.DB
	sm     *StateMachine
	warmup bool
}

func NewEngine(cfg *config.Config, db *store.DB) *Engine {
	return &Engine{
		cfg: cfg,
		db:  db,
		sm:  NewStateMachine(cfg),
	}
}

func (e *Engine) IsWarm(ctx context.Context) (bool, error) {
	first, err := e.db.FirstSampleTime(ctx)
	if err != nil || first == 0 {
		return false, err
	}
	warmupSec := int64(e.cfg.Baseline.WarmupDays) * 86400
	warm := store.NowUnix()-first >= warmupSec
	ready, err := e.db.BaselinesReady(ctx)
	if err != nil {
		return warm, err
	}
	e.warmup = warm && ready
	return e.warmup, nil
}

type ProbeMetrics struct {
	LossPct   float64
	LatencyMs float64
	JitterMs  float64
	OK        float64
	FailCount int
}

func AggregateMetrics(samples []store.Sample) ProbeMetrics {
	var m ProbeMetrics
	var okCount, failCount int
	for _, s := range samples {
		switch s.Metric {
		case "loss_pct":
			m.LossPct = s.Value
		case "latency_ms":
			if s.Value > m.LatencyMs {
				m.LatencyMs = s.Value
			}
		case "jitter_ms":
			m.JitterMs = s.Value
		case "ok":
			if s.Value >= 1 {
				okCount++
			} else {
				failCount++
			}
		}
	}
	m.FailCount = failCount
	if okCount > 0 && failCount == 0 {
		m.OK = 1
	} else if failCount > 0 && okCount == 0 {
		m.OK = 0
	} else if failCount > okCount {
		m.OK = 0
	} else {
		m.OK = 1
	}
	return m
}

func (e *Engine) Evaluate(ctx context.Context, samplesByProbe map[string][]store.Sample) error {
	now := store.NowUnix()
	warm, _ := e.IsWarm(ctx)
	hour := store.HourOfWeek(time.Now())

	dimStates := make(map[string]string)
	dimDetail := make(map[string]map[string]interface{})

	evalGateway := func(probe string, m ProbeMetrics) string {
		th := e.cfg.Threshold.Gateway
		latThresh := effectiveThreshold(ctx, e.db, warm, e.cfg, probe, "latency_ms", hour, th.LatencyMsDegraded, th.LatencyMsDown, true)
		if m.LossPct >= th.LossPctDown || m.OK < 1 {
			return StateDown
		}
		if m.LossPct >= th.LossPctDegraded || m.LatencyMs >= latThresh.degraded {
			return StateDegraded
		}
		return StateOK
	}

	evalDNS := func(probe string, m ProbeMetrics) string {
		th := e.cfg.Threshold.DNS
		lat := effectiveThreshold(ctx, e.db, warm, e.cfg, probe, "latency_ms", hour, th.LatencyMsDegraded, th.LatencyMsDown, true)
		if m.OK < 1 || m.LatencyMs >= lat.down {
			return StateDown
		}
		if m.LatencyMs >= lat.degraded {
			return StateDegraded
		}
		return StateOK
	}

	evalPath := func(probe string, m ProbeMetrics) string {
		th := e.cfg.Threshold.Path
		lat := effectiveThreshold(ctx, e.db, warm, e.cfg, probe, "latency_ms", hour, th.LatencyMsDegraded, th.LatencyMsDown, true)
		if m.FailCount >= th.FailCountDown || m.OK < 1 {
			return StateDown
		}
		if m.LatencyMs >= lat.down {
			return StateDown
		}
		if m.LatencyMs >= lat.degraded {
			return StateDegraded
		}
		return StateOK
	}

	for probe, samples := range samplesByProbe {
		m := AggregateMetrics(samples)
		var st string
		switch {
		case probe == "gateway":
			st = evalGateway(probe, m)
		case probe == "dns" || len(probe) > 4 && probe[:4] == "dns:":
			st = evalDNS(probe, m)
		default:
			st = evalPath(probe, m)
		}
		dimStates[probe] = st
		dimDetail[probe] = map[string]interface{}{
			"metrics":   m,
			"warm":      warm,
			"threshold": true,
		}
	}

	// overall
	var all []string
	for _, st := range dimStates {
		all = append(all, st)
	}
	overall := WorstState(all...)
	dimStates["overall"] = overall
	dimDetail["overall"] = map[string]interface{}{"children": dimStates, "warm": warm}

	// apply state machine + persist
	for dim, proposed := range dimStates {
		current, err := e.sm.Apply(ctx, e.db, dim, proposed, now)
		if err != nil {
			return err
		}
		detail := dimDetail[dim]
		if detail == nil {
			detail = map[string]interface{}{}
		}
		detail["proposed"] = proposed
		if err := e.db.SetState(ctx, dim, current, now, detail); err != nil {
			return err
		}
	}

	return e.handleIncidents(ctx, overall, dimStates, dimDetail, now)
}

type threshPair struct {
	degraded float64
	down     float64
}

func effectiveThreshold(ctx context.Context, db *store.DB, warm bool, cfg *config.Config, probe, metric string, hour int, defDeg, defDown float64, useBaseline bool) threshPair {
	pair := threshPair{degraded: defDeg, down: defDown}
	if !warm || !useBaseline {
		return pair
	}
	bl, err := db.GetBaseline(ctx, probe, metric, hour)
	if err != nil || bl == nil {
		return pair
	}
	anom := bl.P95 * cfg.Baseline.AnomalyMultiplier
	if anom > pair.degraded {
		pair.degraded = anom
	}
	if bl.P95*2 > pair.down {
		pair.down = bl.P95 * 2
	}
	return pair
}

func (e *Engine) handleIncidents(ctx context.Context, overall string, dimStates map[string]string, detail map[string]map[string]interface{}, now int64) error {
	active, err := e.db.ActiveIncident(ctx)
	if err != nil {
		return err
	}

	bad := overall == StateDegraded || overall == StateDown
	if bad && active == nil {
		detailJSON, _ := json.Marshal(map[string]interface{}{
			"dimensions": dimStates,
			"detail":     detail,
			"opened_at":  store.FormatTS(now),
		})
		_, err = e.db.OpenIncident(ctx, now, overall, string(detailJSON))
		return err
	}
	if !bad && active != nil {
		detailJSON, _ := json.Marshal(map[string]interface{}{
			"dimensions": dimStates,
			"detail":     detail,
			"closed_at":  store.FormatTS(now),
			"resolved":   true,
		})
		return e.db.CloseIncident(ctx, active.ID, now, string(detailJSON))
	}
	if bad && active != nil && Rank(overall) > Rank(active.OverallState) {
		closeDetail, _ := json.Marshal(map[string]interface{}{
			"dimensions": dimStates,
			"detail":     detail,
			"escalated":  store.FormatTS(now),
			"new_state":  overall,
		})
		if err := e.db.CloseIncident(ctx, active.ID, now, string(closeDetail)); err != nil {
			return err
		}
		openDetail, _ := json.Marshal(map[string]interface{}{
			"dimensions": dimStates,
			"detail":     detail,
			"opened_at":  store.FormatTS(now),
			"escalated_from": active.ID,
		})
		_, err = e.db.OpenIncident(ctx, now, overall, string(openDetail))
		return err
	}
	return nil
}

// StateMachine applies hysteresis/debounce to proposed states.
type StateMachine struct {
	cfg           *config.Config
	pending       map[string]pendingTransition
	current       map[string]string
}

type pendingTransition struct {
	proposed  string
	since     int64
}

func NewStateMachine(cfg *config.Config) *StateMachine {
	return &StateMachine{
		cfg:     cfg,
		pending: make(map[string]pendingTransition),
		current: make(map[string]string),
	}
}

func (sm *StateMachine) Apply(ctx context.Context, db *store.DB, dim, proposed string, now int64) (string, error) {
	states, err := db.GetStates(ctx)
	if err != nil {
		return proposed, err
	}
	cur := StateOK
	for _, s := range states {
		sm.current[s.Dimension] = s.State
		if s.Dimension == dim {
			cur = s.State
			if cur == "" {
				cur = StateOK
			}
		}
	}
	if _, ok := sm.current[dim]; !ok {
		sm.current[dim] = StateOK
		cur = StateOK
	}

	if proposed == cur {
		delete(sm.pending, dim)
		return cur, nil
	}

	// worsening: apply after debounce
	if Rank(proposed) > Rank(cur) {
		p, ok := sm.pending[dim]
		if !ok || p.proposed != proposed {
			sm.pending[dim] = pendingTransition{proposed: proposed, since: now}
			return cur, nil
		}
		if now-p.since >= int64(sm.cfg.State.Debounce.Std().Seconds()) {
			delete(sm.pending, dim)
			sm.current[dim] = proposed
			return proposed, nil
		}
		return cur, nil
	}

	// improving: clear degraded faster
	clearAfter := sm.cfg.State.ClearDegradedAfter.Std()
	if Rank(proposed) < Rank(cur) {
		p, ok := sm.pending[dim]
		if !ok || p.proposed != proposed {
			sm.pending[dim] = pendingTransition{proposed: proposed, since: now}
			return cur, nil
		}
		wait := sm.cfg.State.Debounce.Std()
		if Rank(cur) == Rank(StateDegraded) && Rank(proposed) == Rank(StateOK) {
			wait = clearAfter
		}
		if now-p.since >= int64(wait.Seconds()) {
			delete(sm.pending, dim)
			sm.current[dim] = proposed
			return proposed, nil
		}
		return cur, nil
	}

	return cur, nil
}

func (e *Engine) BaselineModeLabel(ctx context.Context) string {
	warm, err := e.IsWarm(ctx)
	if err != nil {
		return "learning"
	}
	if warm {
		return "baseline_active"
	}
	return "learning"
}

func SnapshotStatesJSON(states []store.DimensionState) string {
	b, _ := json.Marshal(states)
	return string(b)
}

func FormatEvalError(err error) string {
	return fmt.Sprintf("eval: %v", err)
}
