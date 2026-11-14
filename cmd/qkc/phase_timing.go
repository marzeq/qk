package main

import (
	"fmt"
	"os"
	"time"
)

type phaseTimer struct {
	enabled bool
	start   time.Time
}

func newPhaseTimer(enabled bool) *phaseTimer {
	return &phaseTimer{enabled: enabled, start: time.Now()}
}

func (timer *phaseTimer) mark(name string) {
	if !timer.enabled {
		return
	}
	now := time.Now()
	fmt.Fprintf(os.Stderr, "phase %-22s %8.3f ms\n", name, float64(now.Sub(timer.start))/float64(time.Millisecond))
	timer.start = now
}

func (timer *phaseTimer) count(name string, value int) {
	if timer.enabled {
		fmt.Fprintf(os.Stderr, "count %-22s %8d\n", name, value)
	}
}
