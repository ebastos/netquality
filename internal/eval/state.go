package eval

// State values for dimensions and overall health.
const (
	StateOK       = "ok"
	StateDegraded = "degraded"
	StateDown     = "down"
)

var stateRank = map[string]int{
	StateOK:       0,
	StateDegraded: 1,
	StateDown:     2,
}

// WorstState returns the most severe state among the inputs (down > degraded > ok).
func WorstState(states ...string) string {
	worst := StateOK
	maxRank := 0
	for _, s := range states {
		r, ok := stateRank[s]
		if !ok {
			continue
		}
		if r > maxRank {
			maxRank = r
			worst = s
		}
	}
	return worst
}

// Rank returns the numeric severity rank for a state string (used by StateMachine debounce logic).
func Rank(s string) int {
	if r, ok := stateRank[s]; ok {
		return r
	}
	return 0
}
