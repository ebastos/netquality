# Design: Drill-Down Capabilities for Netquality

**Status:** Approved  
**Date:** 2026-05-15  
**Author:** Grok (brainstorming session 019e2dcd)  
**Goal:** Enable users to understand *why* a layer entered a degraded or down state and to explore historical probe data, while preserving the clean, glanceable, single-scroll Overview that defines the current dashboard.

---

## Problem Statement

The current dashboard excels at at-a-glance monitoring:
- Status pills and overall health
- Layer cards with 26 px sparklines
- One aggregated latency chart
- Incidents table + modal that dumps raw JSON

Operators and power users frequently need to answer:
- "Why did this path probe go degraded at 14:23?"
- "What were the actual latency and loss values compared to the active threshold?"
- "Show me the raw samples around this incident."

Adding this capability must not make the primary monitoring view feel busy or complex.

---

## Design Principles

1. **Overview stays pristine** — No new permanent widgets, charts, or visual weight on the default landing experience.
2. **Progressive disclosure** — Extra detail lives in transient or opt-in surfaces (modal improvements + one dedicated Analysis view).
3. **Incident explanation is primary** — The most common "why did this happen?" question must be answered beautifully in the existing incident flow.
4. **Reuse over invention** — The backend already stores rich `samples`, `rollups_5m`, state `detail`, and incident snapshots. We expose and present that data rather than collecting new telemetry.
5. **Single-probe focus in v1** — Keep the Analysis surface simple; side-by-side comparison is deferred.

---

## Recommended Solution: Analysis Hub + Enhanced Incident Modal + Layer Affordances

### 1. Enrich Backend Detail (Foundation for Honest Explanations)

During evaluation in `internal/eval/eval.go`, the `dimDetail` map already captures `ProbeMetrics` and a `"proposed"` state. We extend it to also record the *actual thresholds that were applied* and their origin:

```json
"thresholds": {
  "latency_ms": { "degraded": 124.5, "down": 380.0, "source": "baseline" },
  "loss_pct":   { "degraded": 2.0,   "down": 10.0,  "source": "config" }
}
```

This flows automatically into:
- The `states.detail` row (current dimension state)
- The `incidents.detail_json` snapshot at open / escalate / close time

No schema change is required.

A small new `GET /api/v1/probes` endpoint (returning distinct probe names + last seen timestamp) makes the future Analysis selector reliable and cheap.

### 2. Upgrade the Incident Details Modal (Highest User Value)

Replace the current raw-JSON dump with a clear, scannable structure:

1. **Header** — ID, overall state, start/end/duration (unchanged)
2. **Timeline** — Parsed events (`opened_at`, `escalated`, `closed_at`, `resolved`, `escalated_from`) shown as a short chronological list
3. **Triggering Measurements** (new) — For every non-OK layer:
   - Probe name + final state badge
   - Observed metrics (latency, loss, jitter, fail count)
   - The exact threshold crossed and its source ("baseline p95 × 1.5" vs "config default")
   - One-sentence explanation: "Latency 187 ms ≥ baseline threshold 124 ms → proposed degraded"
4. **Affected Layers** — Improved visual pills
5. **Technical Detail** — `<details>` disclosure containing the original pretty-printed JSON (power users still have full access)

Add a primary action button: **"View in Analysis"**  
Clicking it activates the Analysis sidebar item, sets the time range to the incident window, and pre-selects the most relevant probe(s).

### 3. Layer Cards — Subtle Entry Point

Layer cards remain visually identical. On hover (or long-press on touch devices) a faint "View details →" affordance appears. Clicking the card (or affordance) activates the Analysis view filtered to that probe and the current time window.

This gives users a natural gesture ("I want to know more about DNS") without adding any permanent visual weight to the Overview.

### 4. New "Analysis" Sidebar Item (Moderate Complexity Accepted)

The sidebar already contains five nav items (most are currently stubs). We add a sixth: **Analysis** (icon: detailed chart or magnifying glass).

When activated, the main content area shows a dedicated Analysis surface instead of the Overview cards and chart. The Overview itself is never mutated.

**Analysis surface contents (single-probe focused):**

- Probe selector (dropdown or pills, sourced from the new `/api/v1/probes` endpoint)
- Time-range selector (reuses the existing 24h / 7d / 30d control)
- Latency chart for the selected probe:
  - Avg line + p95 line (or light band) using the existing rollup data
  - Small incident markers overlaid for context
- Stats row (4 compact cards): current/peak/p95 latency, loss behavior, sample count, effective thresholds right now
- Raw samples table: last 50–100 points fetched via the already-existing `GET /api/v1/samples` endpoint
- Export CSV for the current selection

Navigation is simple: clicking "Overview" in the sidebar returns to the normal dashboard. The periodic refresh loop continues uninterrupted on the Overview while the user is in Analysis.

All new UI elements use the exact same color tokens, card styles, typography, and spacing as the existing design system. The dark Pi-friendly aesthetic is preserved.

---

## Scope

**In scope**
- Backend enrichment of threshold values in evaluation detail
- One new lightweight `GET /api/v1/probes` endpoint
- Significantly improved incident modal with human-readable trigger explanation
- Subtle hover affordance on layer cards that routes to Analysis
- New "Analysis" sidebar surface (single-probe, reuses `/rollups` + `/samples`)
- "View in Analysis" action from the incident modal
- Full test coverage and manual regression on the Overview

**Out of scope (explicitly deferred)**
- Side-by-side probe comparison
- Live-updating charts inside Analysis
- Any new permanent charts or widgets on the main Overview
- Changes to scheduling, retention, baseline computation, or config
- Image/PDF export of Analysis views
- WebSocket or streaming updates

---

## Success Criteria

- The default Overview page is visually and functionally unchanged on first load.
- Any incident that previously existed can now display the concrete metrics and thresholds that caused the state change.
- A user can reach detailed per-probe data (chart + raw samples + current thresholds) in at most two clicks from either a layer card or an incident.
- All existing automated checks (`go vet`, `go test -race`) continue to pass.
- Manual verification on a Pi-class device confirms touch targets remain adequate and the interface stays responsive.

---

## Files Changed (High Level)

| Area | Files |
|------|-------|
| Backend API & evaluation | `internal/api/server.go`, `internal/api/handlers.go`, `internal/eval/eval.go` |
| Data access (optional helper) | `internal/store/sqlite.go` |
| Dashboard markup | `internal/api/web/index.html` |
| Dashboard behavior | `internal/api/web/app.js` (new analysis module + refactored incident modal) |
| Dashboard styles | `internal/api/web/style.css` (new analysis panel styles only) |

All new code follows the existing patterns documented in `AGENTS.md` (route registration in server.go, handler tests, reuse of `StateOK`/`StateDegraded`/`StateDown` constants, etc.).

---

## Next Steps

1. Write `phase-context.yml` for the ACE planning system.
2. Commit this design document.
3. Ask the user whether they are ready to create an isolated git worktree and generate a detailed implementation plan (using the writing-plans-ace skill).

This design was validated through collaborative brainstorming with explicit user preference for "incident explanation first + moderate Analysis surface" and has been approved for implementation.