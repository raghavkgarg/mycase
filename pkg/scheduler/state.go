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
// cadence on the same day.
type State struct {
	// LastRun maps a cadence to the "2006-01-02" trading day it last completed.
	LastRun map[string]string `json:"last_run"`
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

func statePath() string {
	return config.DataPath(stateFileName)
}

// LoadState reads the persisted scheduler state. A missing/corrupt file yields an
// empty State (not an error) so a fresh install starts clean.
func LoadState() (State, error) {
	s := State{LastRun: map[string]string{}}
	data, err := os.ReadFile(statePath())
	if err != nil {
		return s, nil
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return State{LastRun: map[string]string{}}, err
	}
	if s.LastRun == nil {
		s.LastRun = map[string]string{}
	}
	return s, nil
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
