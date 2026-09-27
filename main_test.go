package main

import (
	"context"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestVersionString(t *testing.T) {
	vs := versionString()
	if vs == "" {
		t.Fatal("versionString returned empty string")
	}
	if !strings.Contains(vs, Version) {
		t.Errorf("versionString %q does not contain Version %q", vs, Version)
	}
	if !strings.Contains(vs, "commit:") {
		t.Errorf("versionString %q does not contain 'commit:'", vs)
	}
	if !strings.Contains(vs, "built:") {
		t.Errorf("versionString %q does not contain 'built:'", vs)
	}
}

func TestCommandName(t *testing.T) {
	calledRoot := false
	calledSub := false

	subCmd := &cli.Command{
		Name: "scheduler",
		Action: func(ctx context.Context, c *cli.Command) error {
			calledSub = true
			if name := commandName(c); name != "scheduler" {
				t.Errorf("expected 'scheduler', got %q", name)
			}
			return nil
		},
	}
	app := &cli.Command{
		Name: "mycase",
		Action: func(ctx context.Context, c *cli.Command) error {
			calledRoot = true
			if name := commandName(c); name != "mycase" {
				t.Errorf("expected 'mycase', got %q", name)
			}
			return nil
		},
		Commands: []*cli.Command{
			subCmd,
		},
	}

	if err := app.Run(context.Background(), []string{"mycase"}); err != nil {
		t.Fatalf("app.Run root failed: %v", err)
	}
	if !calledRoot {
		t.Error("expected root command to be called")
	}

	if err := app.Run(context.Background(), []string{"mycase", "scheduler"}); err != nil {
		t.Fatalf("app.Run sub failed: %v", err)
	}
	if !calledSub {
		t.Error("expected sub command to be called")
	}
}
