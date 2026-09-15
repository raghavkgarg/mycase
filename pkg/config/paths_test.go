package config

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestConfigDirEnvOverride(t *testing.T) {
	t.Setenv(configDirEnv, "/custom/cfg")
	if got := ConfigDir(); got != "/custom/cfg" {
		t.Errorf("ConfigDir() = %q, want /custom/cfg", got)
	}
	if got := Path("csvlinks.json"); got != "/custom/cfg/csvlinks.json" {
		t.Errorf("Path() = %q, want /custom/cfg/csvlinks.json", got)
	}
}

func TestDataDirEnvOverride(t *testing.T) {
	t.Setenv(dataDirEnv, "/custom/data")
	if got := DataDir(); got != "/custom/data" {
		t.Errorf("DataDir() = %q, want /custom/data", got)
	}
	if got := DataPath("candidates", "proposals"); got != "/custom/data/candidates/proposals" {
		t.Errorf("DataPath() = %q, want /custom/data/candidates/proposals", got)
	}
}

func TestHomeEnvOverrideDrivesDirs(t *testing.T) {
	// With MYCASE_HOME set and no explicit dir overrides, config/data derive
	// from home. Reset the cached home for this test.
	t.Setenv(homeEnv, "/opt/mycase")
	homeDir = ""
	homeOnce = sync.Once{}

	if got := ConfigDir(); got != filepath.Join("/opt/mycase", "config") {
		t.Errorf("ConfigDir() = %q, want /opt/mycase/config", got)
	}
	if got := DataDir(); got != filepath.Join("/opt/mycase", "data") {
		t.Errorf("DataDir() = %q, want /opt/mycase/data", got)
	}
}
