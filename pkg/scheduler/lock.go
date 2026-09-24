package scheduler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/raghavkgarg/mycase/pkg/config"
)

// lockFileName is the scheduler's single-instance PID lock, under the data dir.
// It guards a RunOnce pass so a stray manual `run-now` overlapping the scheduled
// launchd fire refuses cleanly up front, rather than starting work and then
// colliding on DuckDB's single-writer lock mid-pass (the field incident).
const lockFileName = "scheduler.lock"

// ErrLocked is returned by acquireLock when another live scheduler process holds
// the lock. It carries the holding PID for an actionable message.
type ErrLocked struct{ PID int }

func (e ErrLocked) Error() string {
	return fmt.Sprintf("another scheduler run is in progress (pid %d); refusing to start a second writer", e.PID)
}

// runLock is a held single-instance lock; call release when the pass finishes.
type runLock struct{ path string }

func lockPath() string { return config.DataPath(lockFileName) }

// LockStatus reports whether the single-instance lock is currently held by a live
// process. Used by `scheduler doctor` to surface a stuck/held lock. held is false
// for no lock or a stale lock (dead PID). It never mutates the lock.
func LockStatus() (held bool, pid int, path string) {
	path = lockPath()
	p, ok := readLockPID(path)
	if !ok {
		return false, 0, path
	}
	if !processAlive(p) {
		return false, p, path // stale
	}
	return true, p, path
}

// acquireLock takes the scheduler's single-instance lock. It creates the lock
// file atomically (O_CREATE|O_EXCL) with the current PID. If the file already
// exists, it inspects the recorded PID: a still-running process means the lock is
// live and we return ErrLocked; a dead PID (or an unparseable/empty file) is a
// stale lock left by a crash or SIGKILL — we steal it and proceed. This keeps the
// tool self-healing: a killed run (battery, forced sleep) never wedges the next
// day's fire behind a lock nobody holds.
func acquireLock() (*runLock, error) {
	p := lockPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir for lock: %w", err)
	}

	if l, err := tryCreateLock(p); err == nil {
		return l, nil
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}

	// Lock file exists — decide live vs stale.
	if pid, ok := readLockPID(p); ok && pid != os.Getpid() && processAlive(pid) {
		return nil, ErrLocked{PID: pid}
	}
	// Stale (dead/own/garbage PID): remove and retry once.
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("clearing stale lock %s: %w", p, err)
	}
	return tryCreateLock(p)
}

// tryCreateLock atomically creates the lock file with our PID. Returns
// os.ErrExist (wrapped) if it already exists.
func tryCreateLock(p string) (*runLock, error) {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err // may be os.ErrExist, handled by caller
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		return nil, fmt.Errorf("writing lock pid: %w", err)
	}
	return &runLock{path: p}, nil
}

// release removes the lock file. Best-effort: a failure is not fatal (the next
// run's stale-detection would reclaim it anyway).
func (l *runLock) release() {
	if l == nil {
		return
	}
	_ = os.Remove(l.path)
}

// readLockPID reads the PID recorded in the lock file. ok is false when the file
// is missing, empty, or unparseable (all treated as stale by the caller).
func readLockPID(p string) (pid int, ok bool) {
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// processAlive reports whether a process with the given PID currently exists.
// On Unix, signal 0 probes existence without affecting the process: nil or
// EPERM (exists but not ours to signal) mean alive; ESRCH means gone.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
