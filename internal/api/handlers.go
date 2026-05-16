package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/ebastos/netquality/internal/store"
)

type handlers struct {
	srv *Server
}

type learningInfo struct {
	WarmupDays       int     `json:"warmup_days"`
	FirstSampleTs    int64   `json:"first_sample_ts"`
	DaysCollected    float64 `json:"days_collected"`
	TimeProgress     float64 `json:"time_progress"`
	BaselinesReady   bool    `json:"baselines_ready"`
	BaselineRowCount int     `json:"baseline_row_count"`
}

func (h *handlers) buildLearningInfo(ctx context.Context) (*learningInfo, error) {
	warmupDays := h.srv.cfg.Baseline.WarmupDays
	if warmupDays <= 0 {
		warmupDays = 14
	}
	first, err := h.srv.db.FirstSampleTime(ctx)
	if err != nil {
		return nil, err
	}
	now := store.NowUnix()
	var daysCollected float64
	if first > 0 {
		daysCollected = float64(now-first) / 86400
	}
	var timeProgress float64
	if warmupDays > 0 {
		timeProgress = math.Min(1, daysCollected/float64(warmupDays))
	}
	baselinesReady, err := h.srv.db.BaselinesReady(ctx)
	if err != nil {
		return nil, err
	}
	baselineCount, err := h.srv.db.BaselineCount(ctx)
	if err != nil {
		return nil, err
	}
	return &learningInfo{
		WarmupDays:       warmupDays,
		FirstSampleTs:    first,
		DaysCollected:    daysCollected,
		TimeProgress:     timeProgress,
		BaselinesReady:   baselinesReady,
		BaselineRowCount: baselineCount,
	}, nil
}

func (h *handlers) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	states, err := h.srv.db.GetStates(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	warm, _ := h.srv.engine.IsWarm(ctx)
	mode := h.srv.engine.BaselineModeLabel(ctx)

	type resp struct {
		DeviceID     string                 `json:"device_id"`
		States       []store.DimensionState `json:"states"`
		BaselineMode string                 `json:"baseline_mode"`
		Warm         bool                   `json:"warm"`
		Learning     *learningInfo          `json:"learning,omitempty"`
		GatewayHost  string                 `json:"gateway_host,omitempty"`
		PublicIP     *store.PublicIPInfo    `json:"public_ip,omitempty"`
	}
	out := resp{
		DeviceID:     h.srv.cfg.DeviceID,
		States:       states,
		BaselineMode: mode,
		Warm:         warm,
	}
	if !warm {
		info, err := h.buildLearningInfo(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out.Learning = info
	}
	if h.srv.cfg.PublicIP.Enabled {
		if pip, err := h.srv.db.GetCurrentPublicIP(ctx); err == nil && pip != nil {
			out.PublicIP = pip
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (h *handlers) listIncidents(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			limit = n
		}
	}
	incs, err := h.srv.db.ListIncidents(r.Context(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(incs)
}

func (h *handlers) getIncident(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	inc, err := h.srv.db.GetIncident(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if inc == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(inc)
}

func (h *handlers) exportIncident(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	inc, err := h.srv.db.GetIncident(r.Context(), id)
	if err != nil || inc == nil {
		http.NotFound(w, r)
		return
	}
	bundle, err := h.srv.db.BuildExport(r.Context(), h.srv.cfg.DeviceID, inc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=incident-"+strconv.FormatInt(id, 10)+".json")
	_ = json.NewEncoder(w).Encode(bundle)
}

func (h *handlers) samples(w http.ResponseWriter, r *http.Request) {
	probe := r.URL.Query().Get("probe")
	from := parseTimeQuery(r, "from", time.Now().Add(-24*time.Hour).Unix())
	to := parseTimeQuery(r, "to", time.Now().Unix())
	samples, err := h.srv.db.SamplesRange(r.Context(), probe, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(samples)
}

func (h *handlers) rollups(w http.ResponseWriter, r *http.Request) {
	since := parseTimeQuery(r, "since", time.Now().Add(-24*time.Hour).Unix())
	rollups, err := h.srv.db.RollupsSince(r.Context(), since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rollups)
}

func parseTimeQuery(r *http.Request, key string, def int64) int64 {
	q := r.URL.Query().Get(key)
	if q == "" {
		return def
	}
	if n, err := strconv.ParseInt(q, 10, 64); err == nil {
		return n
	}
	if t, err := time.Parse(time.RFC3339, q); err == nil {
		return t.Unix()
	}
	return def
}
