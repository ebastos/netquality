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

func Rank(s string) int {
	if r, ok := stateRank[s]; ok {
		return r
	}
	return 0
}
