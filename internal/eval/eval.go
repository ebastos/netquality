package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	m.OK = deriveOK(okCount, failCount)
	return m
}

// deriveOK returns 1 if the probe is considered successful overall, 0 otherwise.
// A probe is OK when there are no failures, or when successes >= failures.
func deriveOK(okCount, failCount int) float64 {
	if failCount == 0 || okCount >= failCount {
		return 1
	}
	return 0
}

// classifyProbe returns the evaluation category for a probe name.
// "gateway" and "dns"/"dns:*" are special; everything else is treated as a path probe.
func classifyProbe(probe string) string {
	switch {
	case probe == "gateway":
		return "gateway"
	case probe == "dns" || strings.HasPrefix(probe, "dns:"):
		return "dns"
	default:
		return "path"
	}
}

func (e *Engine) Evaluate(ctx context.Context, samplesByProbe map[string][]store.Sample) error {
	now := store.NowUnix()
	warm, _ := e.IsWarm(ctx)
	hour := store.HourOfWeek(time.Now())

	dimStates := make(map[string]string)
	dimDetail := make(map[string]map[string]any)

	for probe, samples := range samplesByProbe {
		m := AggregateMetrics(samples)
		var st string
		switch classifyProbe(probe) {
		case "gateway":
			st = e.evaluateGateway(ctx, probe, m, warm, hour)
		case "dns":
			st = e.evaluateDNS(ctx, probe, m, warm, hour)
		default:
			st = e.evaluatePath(ctx, probe, m, warm, hour)
		}
		dimStates[probe] = st
		dimDetail[probe] = map[string]any{
			"metrics":   m,
			"warm":      warm,
			"threshold": true,
		}
	}

	// Compute overall worst state across all dimensions.
	var all []string
	for _, st := range dimStates {
		all = append(all, st)
	}
	overall := WorstState(all...)
	dimStates["overall"] = overall
	dimDetail["overall"] = map[string]any{"children": dimStates, "warm": warm}

	// Apply debounce/hysteresis via StateMachine, then persist each dimension's final state.
	for dim, proposed := range dimStates {
		current, err := e.sm.Apply(ctx, e.db, dim, proposed, now)
		if err != nil {
			return err
		}
		detail := dimDetail[dim]
		if detail == nil {
			detail = map[string]any{}
		}
		detail["proposed"] = proposed
		if err := e.db.SetState(ctx, dim, current, now, detail); err != nil {
			return err
		}
	}

	return e.handleIncidents(ctx, overall, dimStates, dimDetail, now)
}

func (e *Engine) evaluateGateway(ctx context.Context, probe string, m ProbeMetrics, warm bool, hour int) string {
	th := e.cfg.Threshold.Gateway
	lat := effectiveThreshold(ctx, e.db, warm, e.cfg, probe, "latency_ms", hour, th.LatencyMsDegraded, th.LatencyMsDown, true)
	if m.LossPct >= th.LossPctDown || m.OK < 1 {
		return StateDown
	}
	if m.LossPct >= th.LossPctDegraded || m.LatencyMs >= lat.degraded {
		return StateDegraded
	}
	return StateOK
}

func (e *Engine) evaluateDNS(ctx context.Context, probe string, m ProbeMetrics, warm bool, hour int) string {
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

func (e *Engine) evaluatePath(ctx context.Context, probe string, m ProbeMetrics, warm bool, hour int) string {
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

func (e *Engine) handleIncidents(ctx context.Context, overall string, dimStates map[string]string, detail map[string]map[string]any, now int64) error {
	active, err := e.db.ActiveIncident(ctx)
	if err != nil {
		return err
	}

	bad := overall == StateDegraded || overall == StateDown

	switch {
	case bad && active == nil:
		return e.openIncident(ctx, overall, dimStates, detail, now)
	case !bad && active != nil:
		return e.closeIncident(ctx, active, dimStates, detail, now, true)
	case bad && active != nil && Rank(overall) > Rank(active.OverallState):
		return e.escalateIncident(ctx, active, overall, dimStates, detail, now)
	default:
		return nil
	}
}

func (e *Engine) openIncident(ctx context.Context, overall string, dimStates map[string]string, detail map[string]map[string]any, now int64) error {
	payload, _ := json.Marshal(map[string]any{
		"dimensions": dimStates,
		"detail":     detail,
		"opened_at":  store.FormatTS(now),
	})
	_, err := e.db.OpenIncident(ctx, now, overall, string(payload))
	return err
}

func (e *Engine) closeIncident(ctx context.Context, active *store.Incident, dimStates map[string]string, detail map[string]map[string]any, now int64, resolved bool) error {
	payload, _ := json.Marshal(map[string]any{
		"dimensions": dimStates,
		"detail":     detail,
		"closed_at":  store.FormatTS(now),
		"resolved":   resolved,
	})
	return e.db.CloseIncident(ctx, active.ID, now, string(payload))
}

func (e *Engine) escalateIncident(ctx context.Context, active *store.Incident, overall string, dimStates map[string]string, detail map[string]map[string]any, now int64) error {
	closePayload, _ := json.Marshal(map[string]any{
		"dimensions": dimStates,
		"detail":     detail,
		"escalated":  store.FormatTS(now),
		"new_state":  overall,
	})
	if err := e.db.CloseIncident(ctx, active.ID, now, string(closePayload)); err != nil {
		return err
	}
	openPayload, _ := json.Marshal(map[string]any{
		"dimensions":     dimStates,
		"detail":         detail,
		"opened_at":      store.FormatTS(now),
		"escalated_from": active.ID,
	})
	_, err := e.db.OpenIncident(ctx, now, overall, string(openPayload))
	return err
}

// StateMachine applies hysteresis/debounce to proposed states.
type StateMachine struct {
	cfg     *config.Config
	pending map[string]pendingTransition
	current map[string]string
}

type pendingTransition struct {
	proposed string
	since    int64
}

func NewStateMachine(cfg *config.Config) *StateMachine {
	return &StateMachine{
		cfg:     cfg,
		pending: make(map[string]pendingTransition),
		current: make(map[string]string),
	}
}

func (sm *StateMachine) Apply(ctx context.Context, db *store.DB, dim, proposed string, now int64) (string, error) {
	if err := sm.ensureCurrent(ctx, db); err != nil {
		return proposed, err
	}

	cur := sm.currentState(dim)

	if proposed == cur {
		delete(sm.pending, dim)
		return cur, nil
	}

	if Rank(proposed) > Rank(cur) {
		return sm.handleWorsening(dim, cur, proposed, now)
	}
	if Rank(proposed) < Rank(cur) {
		return sm.handleImproving(dim, cur, proposed, now)
	}
	return cur, nil
}

// ensureCurrent refreshes the in-memory current-state cache from the DB.
// This is called on every Apply to pick up external state changes.
func (sm *StateMachine) ensureCurrent(ctx context.Context, db *store.DB) error {
	states, err := db.GetStates(ctx)
	if err != nil {
		return err
	}
	for _, s := range states {
		sm.current[s.Dimension] = s.State
	}
	return nil
}

// currentState returns the cached state for dim, defaulting to StateOK.
func (sm *StateMachine) currentState(dim string) string {
	if cur, ok := sm.current[dim]; ok && cur != "" {
		return cur
	}
	sm.current[dim] = StateOK
	return StateOK
}

// handleWorsening starts or continues a pending transition to a worse state.
// The worse state is only returned after the configured Debounce period.
func (sm *StateMachine) handleWorsening(dim, cur, proposed string, now int64) (string, error) {
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

// handleImproving starts or continues a pending transition to a better state.
// Degraded→OK uses ClearDegradedAfter; all other improvements use Debounce.
func (sm *StateMachine) handleImproving(dim, cur, proposed string, now int64) (string, error) {
	p, ok := sm.pending[dim]
	if !ok || p.proposed != proposed {
		sm.pending[dim] = pendingTransition{proposed: proposed, since: now}
		return cur, nil
	}
	wait := sm.cfg.State.Debounce.Std()
	if Rank(cur) == Rank(StateDegraded) && Rank(proposed) == Rank(StateOK) {
		wait = sm.cfg.State.ClearDegradedAfter.Std()
	}
	if now-p.since >= int64(wait.Seconds()) {
		delete(sm.pending, dim)
		sm.current[dim] = proposed
		return proposed, nil
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
