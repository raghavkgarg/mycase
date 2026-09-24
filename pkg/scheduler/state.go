package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/raghavkgarg/mycase/pkg/config"
)

// stateFileName is the scheduler's persisted last-run state, under the data dir.
const stateFileName = "scheduler_state.json"

// State records the last trading day each cadence completed, so the scheduler can
// detect missed ticks (catch-up) across restarts and avoid double-running a
// cadence on the same day. It also tracks consecutive failures per cadence so the
// scheduler can alert on a persistent (or auth) failure instead of failing
// silently forever — the set-and-forget risk for a daily tool.
type State struct {
	// LastRun maps a cadence to the "2006-01-02" trading day it last completed.
	LastRun map[string]string `json:"last_run"`

	// Failures maps a cadence to its count of consecutive failed attempts since
	// the last success. Reset to 0 on any success.
	Failures map[string]int `json:"failures,omitempty"`

	// Alerted maps a cadence to true once a failure alert has been dispatched for
	// the current failure streak, so the operator is notified once per streak
	// rather than every day. Cleared on the next success.
	Alerted map[string]bool `json:"alerted,omitempty"`
}

func (s *State) lastRun(c Cadence) string {
	if s.LastRun == nil {
		return ""
	}
	return s.LastRun[string(c)]
}

func (s *State) markRun(c Cadence, day string) {
	if s.LastRun == nil {
		s.LastRun = map[string]string{}
	}
	s.LastRun[string(c)] = day
}

// recordSuccess clears the failure streak + alerted flag for a cadence.
func (s *State) recordSuccess(c Cadence) {
	delete(s.Failures, string(c))
	delete(s.Alerted, string(c))
}

// recordFailure increments a cadence's consecutive-failure count and returns the
// new count.
func (s *State) recordFailure(c Cadence) int {
	if s.Failures == nil {
		s.Failures = map[string]int{}
	}
	s.Failures[string(c)]++
	return s.Failures[string(c)]
}

// failCount returns the current consecutive-failure count for a cadence.
func (s *State) failCount(c Cadence) int {
	if s.Failures == nil {
		return 0
	}
	return s.Failures[string(c)]
}

// alreadyAlerted reports whether an alert has been sent for the current streak.
func (s *State) alreadyAlerted(c Cadence) bool {
	return s.Alerted != nil && s.Alerted[string(c)]
}

// markAlerted records that a failure alert was dispatched for the current streak.
func (s *State) markAlerted(c Cadence) {
	if s.Alerted == nil {
		s.Alerted = map[string]bool{}
	}
	s.Alerted[string(c)] = true
}

func statePath() string {
	return config.DataPath(stateFileName)
}

// LoadState reads the persisted scheduler state. A missing/corrupt file yields an
// empty State (not an error) so a fresh install starts clean.
func LoadState() (State, error) {
	s := emptyState()
	data, err := os.ReadFile(statePath())
	if err != nil {
		return s, nil
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return emptyState(), err
	}
	if s.LastRun == nil {
		s.LastRun = map[string]string{}
	}
	if s.Failures == nil {
		s.Failures = map[string]int{}
	}
	if s.Alerted == nil {
		s.Alerted = map[string]bool{}
	}
	return s, nil
}

// emptyState returns a State with all maps initialised.
func emptyState() State {
	return State{
		LastRun:  map[string]string{},
		Failures: map[string]int{},
		Alerted:  map[string]bool{},
	}
}

// SaveState persists the scheduler state (best-effort; creates the data dir).
func SaveState(s State) error {
	p := statePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
