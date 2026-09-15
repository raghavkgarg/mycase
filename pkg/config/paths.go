package config

import (
	"os"
	"path/filepath"
	"sync"
)

// Home / path resolution.
//
// mycase reads reference config from a "config/" directory and reads/writes
// runtime state under a "data/" directory. Both are resolved relative to a
// single "home" root so the binary works whether it is run from the project
// tree, from dist/, or installed (e.g. symlinked into /usr/local/bin).
//
// Resolution precedence for the home root (mirrors the sibling jira-task-manager
// tool and the project's flag>env>default convention):
//
//  1. $MYCASE_HOME — explicit override.
//  2. Binary-relative: resolve the executable (following symlinks) and walk up
//     from <home>/dist/mycase to <home>, accepted only if <home>/config exists.
//     This makes a symlinked /usr/local/bin/mycase find the real project tree.
//  3. Current working directory — backward-compatible with running from the
//     project root.
//
// A resolved home of "." keeps paths relative (the historical behavior).
//
// Individual leaf packages (cache, pithistory, themedb) retain their own
// DefaultDBPath + MYCASE_DB env fallback for when they are invoked directly
// (tests, library use); the composition root passes resolved paths down.

const (
	// homeEnv overrides the resolved home root.
	homeEnv = "MYCASE_HOME"
	// configDirEnv overrides just the config directory (absolute or relative).
	configDirEnv = "MYCASE_CONFIG_DIR"
	// dataDirEnv overrides just the data directory (absolute or relative).
	dataDirEnv = "MYCASE_DATA_DIR"

	configDirName = "config"
	dataDirName   = "data"
)

var (
	homeOnce sync.Once
	homeDir  string
)

// Home returns the resolved home root directory (see package doc for
// precedence). The result is cached after the first call.
func Home() string {
	homeOnce.Do(func() { homeDir = resolveHome() })
	return homeDir
}

func resolveHome() string {
	// 1. Explicit override.
	if h := os.Getenv(homeEnv); h != "" {
		return h
	}
	// 2. Binary-relative, validated by the presence of a config/ dir.
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			// Binary lives at <home>/dist/mycase → up two levels is <home>.
			candidate := filepath.Dir(filepath.Dir(real))
			if isDir(filepath.Join(candidate, configDirName)) {
				return candidate
			}
			// Also accept the binary sitting directly in <home> (e.g. go install
			// into a dir that also holds config/).
			if parent := filepath.Dir(real); isDir(filepath.Join(parent, configDirName)) {
				return parent
			}
		}
	}
	// 3. CWD fallback (running from the project root).
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ConfigDir returns the resolved config directory: $MYCASE_CONFIG_DIR if set,
// otherwise <Home>/config.
func ConfigDir() string {
	if d := os.Getenv(configDirEnv); d != "" {
		return d
	}
	return filepath.Join(Home(), configDirName)
}

// DataDir returns the resolved data directory: $MYCASE_DATA_DIR if set,
// otherwise <Home>/data.
func DataDir() string {
	if d := os.Getenv(dataDirEnv); d != "" {
		return d
	}
	return filepath.Join(Home(), dataDirName)
}

// Path returns the absolute-or-relative path to a config file under ConfigDir
// (e.g. Path("csvlinks.json") → "<home>/config/csvlinks.json").
func Path(elem ...string) string {
	return filepath.Join(append([]string{ConfigDir()}, elem...)...)
}

// DataPath returns the path to a file/dir under DataDir
// (e.g. DataPath("mycase.db"), DataPath("candidates", "proposals")).
func DataPath(elem ...string) string {
	return filepath.Join(append([]string{DataDir()}, elem...)...)
}
