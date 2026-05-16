package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Sample is a single measurement for a probe/metric at a point in time.
type Sample struct {
	Ts     int64             `json:"ts"`
	Probe  string            `json:"probe"`
	Metric string            `json:"metric"`
	Value  float64           `json:"value"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Rollup5m is a 5-minute aggregate (min/max/avg/p95) for a probe/metric bucket.
type Rollup5m struct {
	BucketTs int64   `json:"bucket_ts"`
	Probe    string  `json:"probe"`
	Metric   string  `json:"metric"`
	MinVal   float64 `json:"min_val"`
	MaxVal   float64 `json:"max_val"`
	AvgVal   float64 `json:"avg_val"`
	P95Val   float64 `json:"p95_val"`
	Count    int64   `json:"count"`
}

// Baseline holds the learned P50/P95 values for a probe/metric at a specific hour of the week.
type Baseline struct {
	Probe      string
	Metric     string
	HourOfWeek int
	P50        float64
	P95        float64
	UpdatedAt  int64
}

// DimensionState captures the current health state of one dimension (gateway, dns, path probe, or overall).
type DimensionState struct {
	Dimension string         `json:"dimension"`
	State     string         `json:"state"`
	SinceTs   int64          `json:"since_ts"`
	Detail    map[string]any `json:"detail,omitempty"`
}

// Incident represents a period when overall health was not OK (degraded or down).
type Incident struct {
	ID           int64  `json:"id"`
	StartTs      int64  `json:"start_ts"`
	EndTs        *int64 `json:"end_ts,omitempty"`
	OverallState string `json:"overall_state"`
	DetailJSON   string `json:"detail_json,omitempty"`
}

// PublicIPChange is one historical public IP observation (used for recent list in status).
type PublicIPChange struct {
	Ts         int64  `json:"ts"`
	IP         string `json:"ip"`
	PreviousIP string `json:"previous_ip,omitempty"`
	CGNAT      bool   `json:"cgnat"`
}

// PublicIPInfo is the current public IP state returned by the API (and stored via meta + changes table).
type PublicIPInfo struct {
	IP         string           `json:"ip,omitempty"`
	ChangedAt  int64            `json:"changed_at,omitempty"`
	PreviousIP string           `json:"previous_ip,omitempty"`
	CGNAT      bool             `json:"cgnat"`
	Recent     []PublicIPChange `json:"recent,omitempty"`
}

// DB is the SQLite-backed persistence layer for samples, rollups, baselines, states, and incidents.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the database at the given path and runs migrations.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &DB{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *DB) Close() error {
	return s.db.Close()
}

func (s *DB) migrate() error {
	_, err := s.db.Exec(schemaSQL)
	return err
}

func (s *DB) InsertSamples(ctx context.Context, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO samples (ts, probe, metric, value, labels) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, smp := range samples {
		labels, _ := json.Marshal(smp.Labels)
		if labels == nil {
			labels = []byte("{}")
		}
		if _, err := stmt.ExecContext(ctx, smp.Ts, smp.Probe, smp.Metric, smp.Value, string(labels)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *DB) SamplesRange(ctx context.Context, probe string, from, to int64) ([]Sample, error) {
	q := `SELECT ts, probe, metric, value, labels FROM samples WHERE ts >= ? AND ts <= ?`
	args := []any{from, to}
	if probe != "" {
		q += ` AND probe = ?`
		args = append(args, probe)
	}
	q += ` ORDER BY ts`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sample
	for rows.Next() {
		var smp Sample
		var labelsJSON string
		if err := rows.Scan(&smp.Ts, &smp.Probe, &smp.Metric, &smp.Value, &labelsJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labelsJSON), &smp.Labels)
		out = append(out, smp)
	}
	return out, rows.Err()
}

func (s *DB) RecentSamplesByProbe(ctx context.Context, since int64) (map[string][]Sample, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ts, probe, metric, value, labels FROM samples WHERE ts >= ? ORDER BY probe, ts`,
		since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]Sample)
	for rows.Next() {
		var smp Sample
		var labelsJSON string
		if err := rows.Scan(&smp.Ts, &smp.Probe, &smp.Metric, &smp.Value, &labelsJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labelsJSON), &smp.Labels)
		out[smp.Probe] = append(out[smp.Probe], smp)
	}
	return out, rows.Err()
}

func (s *DB) UpsertRollup(ctx context.Context, r Rollup5m) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rollups_5m (bucket_ts, probe, metric, min_val, max_val, avg_val, p95_val, count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(bucket_ts, probe, metric) DO UPDATE SET
			min_val=excluded.min_val, max_val=excluded.max_val, avg_val=excluded.avg_val,
			p95_val=excluded.p95_val, count=excluded.count`,
		r.BucketTs, r.Probe, r.Metric, r.MinVal, r.MaxVal, r.AvgVal, r.P95Val, r.Count)
	return err
}

func (s *DB) RollupsSince(ctx context.Context, since int64) ([]Rollup5m, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT bucket_ts, probe, metric, min_val, max_val, avg_val, p95_val, count
		FROM rollups_5m WHERE bucket_ts >= ? ORDER BY bucket_ts`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rollup5m
	for rows.Next() {
		var r Rollup5m
		if err := rows.Scan(&r.BucketTs, &r.Probe, &r.Metric, &r.MinVal, &r.MaxVal, &r.AvgVal, &r.P95Val, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *DB) UpsertBaseline(ctx context.Context, b Baseline) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO baselines (probe, metric, hour_of_week, p50, p95, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(probe, metric, hour_of_week) DO UPDATE SET
			p50=excluded.p50, p95=excluded.p95, updated_at=excluded.updated_at`,
		b.Probe, b.Metric, b.HourOfWeek, b.P50, b.P95, b.UpdatedAt)
	return err
}

func (s *DB) GetBaseline(ctx context.Context, probe, metric string, hourOfWeek int) (*Baseline, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT probe, metric, hour_of_week, p50, p95, updated_at FROM baselines
		WHERE probe = ? AND metric = ? AND hour_of_week = ?`,
		probe, metric, hourOfWeek)
	var b Baseline
	if err := row.Scan(&b.Probe, &b.Metric, &b.HourOfWeek, &b.P50, &b.P95, &b.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

func (s *DB) BaselinesReady(ctx context.Context) (bool, error) {
	n, err := s.BaselineCount(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *DB) BaselineCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM baselines`).Scan(&n)
	return n, err
}

func (s *DB) BaselineSamples(ctx context.Context, since int64) ([]Sample, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ts, probe, metric, value, labels FROM samples WHERE ts >= ? ORDER BY ts`,
		since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sample
	for rows.Next() {
		var smp Sample
		var labelsJSON string
		if err := rows.Scan(&smp.Ts, &smp.Probe, &smp.Metric, &smp.Value, &labelsJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labelsJSON), &smp.Labels)
		out = append(out, smp)
	}
	return out, rows.Err()
}

// SetState upserts the current state for a dimension (used by the debounced StateMachine).
func (s *DB) SetState(ctx context.Context, dim, state string, since int64, detail map[string]any) error {
	detailJSON, _ := json.Marshal(detail)
	if detailJSON == nil {
		detailJSON = []byte("{}")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO states (dimension, state, since_ts, detail) VALUES (?, ?, ?, ?)
		ON CONFLICT(dimension) DO UPDATE SET state=excluded.state, since_ts=excluded.since_ts, detail=excluded.detail`,
		dim, state, since, string(detailJSON))
	return err
}

// GetStates returns the latest known state for every dimension.
func (s *DB) GetStates(ctx context.Context) ([]DimensionState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT dimension, state, since_ts, detail FROM states ORDER BY dimension`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DimensionState
	for rows.Next() {
		var ds DimensionState
		var detailJSON string
		if err := rows.Scan(&ds.Dimension, &ds.State, &ds.SinceTs, &detailJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(detailJSON), &ds.Detail)
		out = append(out, ds)
	}
	return out, rows.Err()
}

// OpenIncident creates a new incident and returns its ID.
func (s *DB) OpenIncident(ctx context.Context, startTs int64, overallState, detailJSON string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO incidents (start_ts, overall_state, detail_json) VALUES (?, ?, ?)`,
		startTs, overallState, detailJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// CloseIncident marks an incident as resolved with an end timestamp.
func (s *DB) CloseIncident(ctx context.Context, id, endTs int64, detailJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE incidents SET end_ts = ?, detail_json = ? WHERE id = ? AND end_ts IS NULL`,
		endTs, detailJSON, id)
	return err
}

// ActiveIncident returns the currently open incident, if any.
func (s *DB) ActiveIncident(ctx context.Context) (*Incident, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, start_ts, end_ts, overall_state, detail_json FROM incidents WHERE end_ts IS NULL ORDER BY id DESC LIMIT 1`)
	return scanIncident(row)
}

func (s *DB) GetIncident(ctx context.Context, id int64) (*Incident, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, start_ts, end_ts, overall_state, detail_json FROM incidents WHERE id = ?`, id)
	return scanIncident(row)
}

func scanIncident(row *sql.Row) (*Incident, error) {
	var inc Incident
	var endTs sql.NullInt64
	if err := row.Scan(&inc.ID, &inc.StartTs, &endTs, &inc.OverallState, &inc.DetailJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if endTs.Valid {
		v := endTs.Int64
		inc.EndTs = &v
	}
	return &inc, nil
}

// ListIncidents returns the most recent incidents (newest first), up to limit.
func (s *DB) ListIncidents(ctx context.Context, limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, start_ts, end_ts, overall_state, detail_json FROM incidents
		ORDER BY start_ts DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var inc Incident
		var endTs sql.NullInt64
		if err := rows.Scan(&inc.ID, &inc.StartTs, &endTs, &inc.OverallState, &inc.DetailJSON); err != nil {
			return nil, err
		}
		if endTs.Valid {
			v := endTs.Int64
			inc.EndTs = &v
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

func (s *DB) FirstSampleTime(ctx context.Context) (int64, error) {
	var ts sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MIN(ts) FROM samples`).Scan(&ts)
	if err != nil || !ts.Valid {
		return 0, err
	}
	return ts.Int64, nil
}

func (s *DB) PruneRaw(ctx context.Context, before int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM samples WHERE ts < ?`, before)
	return err
}

func (s *DB) PruneRollups(ctx context.Context, before int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rollups_5m WHERE bucket_ts < ?`, before)
	return err
}

// BuildRollupsFromSamples aggregates samples into 5m buckets for the given window.
func BuildRollupsFromSamples(samples []Sample, bucketSec int64) []Rollup5m {
	type key struct {
		bucket int64
		probe  string
		metric string
	}
	groups := make(map[key][]float64)
	for _, smp := range samples {
		bucket := (smp.Ts / bucketSec) * bucketSec
		k := key{bucket: bucket, probe: smp.Probe, metric: smp.Metric}
		groups[k] = append(groups[k], smp.Value)
	}
	var rollups []Rollup5m
	for k, vals := range groups {
		sort.Float64s(vals)
		minV, maxV := vals[0], vals[len(vals)-1]
		var sum float64
		for _, v := range vals {
			sum += v
		}
		avg := sum / float64(len(vals))
		p95 := Percentile(vals, 0.95)
		rollups = append(rollups, Rollup5m{
			BucketTs: k.bucket,
			Probe:    k.probe,
			Metric:   k.metric,
			MinVal:   minV,
			MaxVal:   maxV,
			AvgVal:   avg,
			P95Val:   p95,
			Count:    int64(len(vals)),
		})
	}
	return rollups
}

// Percentile returns p-th percentile from a sorted slice (nearest-rank, inclusive).
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := max(int(math.Ceil(p*float64(len(sorted))))-1, 0)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func HourOfWeek(t time.Time) int {
	// Sunday=0 .. Saturday=6, hour 0-23 => 0..167
	weekday := int(t.Weekday())
	return weekday*24 + t.Hour()
}

func NowUnix() int64 {
	return time.Now().Unix()
}

func FormatTS(ts int64) string {
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}

// ExportBundle is the complete snapshot returned by the incident export API (incident + states + samples + rollups).
type ExportBundle struct {
	DeviceID   string           `json:"device_id"`
	Incident   Incident         `json:"incident"`
	States     []DimensionState `json:"states"`
	Samples    []Sample         `json:"samples"`
	Rollups    []Rollup5m       `json:"rollups"`
	ExportedAt string           `json:"exported_at"`
	Extra      map[string]any   `json:"extra,omitempty"`
}

// BuildExport assembles the full export bundle for a given incident (used by /incidents/{id}/export).
func (s *DB) BuildExport(ctx context.Context, deviceID string, inc *Incident) (*ExportBundle, error) {
	if inc == nil {
		return nil, fmt.Errorf("incident is nil")
	}
	end := NowUnix()
	if inc.EndTs != nil {
		end = *inc.EndTs
	}
	samples, err := s.SamplesRange(ctx, "", inc.StartTs, end)
	if err != nil {
		return nil, err
	}
	rollups, err := s.RollupsSince(ctx, inc.StartTs)
	if err != nil {
		return nil, err
	}
	states, err := s.GetStates(ctx)
	if err != nil {
		return nil, err
	}
	return &ExportBundle{
		DeviceID:   deviceID,
		Incident:   *inc,
		States:     states,
		Samples:    samples,
		Rollups:    rollups,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// --- Public IP tracking (meta for current snapshot + public_ip_changes table for history) ---

func (s *DB) getMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *DB) setMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *DB) GetCurrentPublicIP(ctx context.Context) (*PublicIPInfo, error) {
	ip, _ := s.getMeta(ctx, "public_ip_v4")
	if ip == "" {
		return nil, nil
	}
	changedStr, _ := s.getMeta(ctx, "public_ip_v4_changed")
	prev, _ := s.getMeta(ctx, "public_ip_v4_prev")
	cgnatStr, _ := s.getMeta(ctx, "public_ip_v4_cgnat")

	var changed int64
	if changedStr != "" {
		// best effort; ignore parse error
		fmt.Sscan(changedStr, &changed)
	}
	cgnat := cgnatStr == "1"

	// fetch up to 5 most recent changes for the response
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts, ip, previous_ip, cgnat FROM public_ip_changes
		ORDER BY ts DESC LIMIT 5`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recent []PublicIPChange
	for rows.Next() {
		var ch PublicIPChange
		var cgnatInt int
		var prevIP sql.NullString
		if err := rows.Scan(&ch.Ts, &ch.IP, &prevIP, &cgnatInt); err != nil {
			return nil, err
		}
		if prevIP.Valid {
			ch.PreviousIP = prevIP.String
		}
		ch.CGNAT = cgnatInt != 0
		recent = append(recent, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &PublicIPInfo{
		IP:         ip,
		ChangedAt:  changed,
		PreviousIP: prev,
		CGNAT:      cgnat,
		Recent:     recent,
	}, nil
}

func (s *DB) RecordPublicIPObservation(ctx context.Context, ts int64, ip string, cgnat bool) error {
	if ip == "" {
		return nil
	}

	cur, _ := s.getMeta(ctx, "public_ip_v4")
	if cur == ip {
		return nil // no change
	}

	prev := cur
	cgnatInt := 0
	if cgnat {
		cgnatInt = 1
	}

	// update current snapshot in meta
	if err := s.setMeta(ctx, "public_ip_v4", ip); err != nil {
		return err
	}
	if err := s.setMeta(ctx, "public_ip_v4_changed", fmt.Sprintf("%d", ts)); err != nil {
		return err
	}
	if err := s.setMeta(ctx, "public_ip_v4_cgnat", fmt.Sprintf("%d", cgnatInt)); err != nil {
		return err
	}
	if prev != "" {
		if err := s.setMeta(ctx, "public_ip_v4_prev", prev); err != nil {
			return err
		}
	}

	// record history row
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO public_ip_changes (ts, ip, previous_ip, cgnat) VALUES (?, ?, ?, ?)`,
		ts, ip, prev, cgnatInt)
	if err != nil {
		return err
	}

	// keep only the most recent 50 changes (history is small; no retention config needed)
	_, _ = s.db.ExecContext(ctx, `
		DELETE FROM public_ip_changes
		WHERE ts NOT IN (SELECT ts FROM public_ip_changes ORDER BY ts DESC LIMIT 50)`)

	return nil
}
