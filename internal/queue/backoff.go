package queue

import "time"

// MaxAttempts is the cap on tries before a message is dead-lettered.
const MaxAttempts = 6

// backoffSchedule is the delay applied AFTER the Nth attempt fails, before
// attempt N+1. Index 0 = delay after the 1st failure.
var backoffSchedule = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	1 * time.Hour,
	6 * time.Hour,
	24 * time.Hour,
}

// NextDelay returns the wait before retrying given the number of attempts
// already made. attemptsMade is 1-indexed (i.e. 1 means one failure has
// just occurred). Returns 0 with ok=false when the message should be
// dead-lettered.
func NextDelay(attemptsMade int) (time.Duration, bool) {
	if attemptsMade <= 0 || attemptsMade >= MaxAttempts {
		return 0, false
	}
	idx := attemptsMade - 1
	if idx >= len(backoffSchedule) {
		return 0, false
	}
	return backoffSchedule[idx], true
}
