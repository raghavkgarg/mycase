package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A fresh acquire succeeds and writes our PID; release removes the file.
func TestLock_AcquireAndRelease(t *testing.T) {
	isolateState(t)

	l, err := acquireLock()
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}
	if _, statErr := os.Stat(lockPath()); statErr != nil {
		t.Fatalf("lock file not created: %v", statErr)
	}
	pid, ok := readLockPID(lockPath())
	if !ok || pid != os.Getpid() {
		t.Fatalf("lock pid = %d ok=%v, want our pid %d", pid, ok, os.Getpid())
	}

	l.release()
	if _, statErr := os.Stat(lockPath()); !os.IsNotExist(statErr) {
		t.Fatalf("lock file should be gone after release, stat err = %v", statErr)
	}
}

// A live foreign holder makes acquire refuse with ErrLocked carrying the PID.
func TestLock_RefusesWhenHeldByLiveProcess(t *testing.T) {
	isolateState(t)

	// PID 1 (launchd/init) is always alive and is not us — simulate a live holder.
	writeLock(t, 1)

	_, err := acquireLock()
	locked := ErrLocked{}
	if !errors.As(err, &locked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
	if locked.PID != 1 {
		t.Fatalf("ErrLocked.PID = %d, want 1", locked.PID)
	}
}

// A stale lock (dead PID) is reclaimed: acquire steals it and succeeds.
func TestLock_ReclaimsStaleLock(t *testing.T) {
	isolateState(t)

	// A PID that is essentially certain to be dead.
	writeLock(t, 0x7FFFFFFE)

	l, err := acquireLock()
	if err != nil {
		t.Fatalf("expected stale lock to be reclaimed, got %v", err)
	}
	pid, ok := readLockPID(lockPath())
	if !ok || pid != os.Getpid() {
		t.Fatalf("after reclaim, lock pid = %d ok=%v, want our pid", pid, ok)
	}
	l.release()
}

// Garbage lock contents are treated as stale and reclaimed.
func TestLock_ReclaimsGarbageLock(t *testing.T) {
	isolateState(t)
	if err := os.MkdirAll(filepath.Dir(lockPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath(), []byte("not-a-pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := acquireLock()
	if err != nil {
		t.Fatalf("garbage lock should be reclaimed, got %v", err)
	}
	l.release()
}

// LockStatus reflects held / stale / absent.
func TestLockStatus(t *testing.T) {
	isolateState(t)

	if held, _, _ := LockStatus(); held {
		t.Fatal("no lock should be held initially")
	}

	writeLock(t, 1) // live foreign holder
	if held, pid, _ := LockStatus(); !held || pid != 1 {
		t.Fatalf("LockStatus held=%v pid=%d, want held pid 1", held, pid)
	}

	writeLock(t, 0x7FFFFFFE) // dead
	if held, pid, _ := LockStatus(); held || pid != 0x7FFFFFFE {
		t.Fatalf("LockStatus for dead pid: held=%v pid=%d, want not-held with the pid reported", held, pid)
	}
}

// writeLock writes a lock file containing pid.
func writeLock(t *testing.T, pid int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(lockPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath(), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
