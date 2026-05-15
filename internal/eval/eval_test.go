package eval

import (
	"testing"
)

func TestWorstState(t *testing.T) {
	if g := WorstState(StateOK, StateDegraded); g != StateDegraded {
		t.Fatalf("got %s", g)
	}
	if g := WorstState(StateOK, StateDown, StateDegraded); g != StateDown {
		t.Fatalf("got %s", g)
	}
}

func TestRank(t *testing.T) {
	if Rank(StateDown) <= Rank(StateOK) {
		t.Fatal("down should rank higher")
	}
}
