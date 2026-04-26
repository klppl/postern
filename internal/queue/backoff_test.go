package queue

import (
	"testing"
	"time"
)

func TestNextDelaySchedule(t *testing.T) {
	cases := []struct {
		attempts int
		want     time.Duration
		ok       bool
	}{
		{0, 0, false},
		{1, time.Minute, true},
		{2, 5 * time.Minute, true},
		{3, 15 * time.Minute, true},
		{4, time.Hour, true},
		{5, 6 * time.Hour, true},
		{6, 0, false}, // dead after 6th attempt
		{7, 0, false},
	}
	for _, c := range cases {
		got, ok := NextDelay(c.attempts)
		if got != c.want || ok != c.ok {
			t.Errorf("NextDelay(%d) = (%v, %v), want (%v, %v)", c.attempts, got, ok, c.want, c.ok)
		}
	}
}
