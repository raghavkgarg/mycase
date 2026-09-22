package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/render"
	"github.com/raghavkgarg/mycase/pkg/scheduler"
)

// SchedulerCommand is the autonomous orchestrator (Phase 12): one long-lived
// process that owns the daily EOD update, the daily drift check, and the
// quarterly/monthly rebalance proposal — replacing the separate `daemon install`
// and `autopilot install` units with a single keep-alive service.
var SchedulerCommand = &cli.Command{
	Name:  "scheduler",
	Usage: "Autonomous orchestrator for EOD / drift / rebalance cadences",
	Commands: []*cli.Command{
		schedulerRunCmd,
		schedulerStatusCmd,
		schedulerInstallCmd,
		schedulerUninstallCmd,
	},
}

var schedulerRunCmd = &cli.Command{
	Name:  "run",
	Usage: "Run the scheduler loop (blocks until stopped; use install for a system service)",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "live", Usage: "Use the live broker API (default: mock)"},
		&cli.StringFlag{Name: "config", Value: config.Path("pipeline.yaml"), Usage: "Pipeline config file"},
		&cli.StringFlag{Name: "file", Usage: "Portfolio CSV for the drift check (overrides config)"},
	},
	Action: runScheduler,
}

var schedulerStatusCmd = &cli.Command{
	Name:   "status",
	Usage:  "Show the last completed trading day per cadence",
	Action: runSchedulerStatus,
}

var schedulerInstallCmd = &cli.Command{
	Name:   "install",
	Usage:  "Install the scheduler as a keep-alive service (launchd on macOS; systemd unit printed on Linux)",
	Action: runSchedulerInstall,
}

var schedulerUninstallCmd = &cli.Command{
	Name:   "uninstall",
	Usage:  "Remove the installed scheduler service",
	Action: runSchedulerUninstall,
}

// buildSchedulerConfig assembles a scheduler.Config from defaults.json (cadence
// toggles), pipeline.yaml (rebalance schedule + alerts), the active broker, and
// the active market's holiday-aware clock.
func buildSchedulerConfig(c *cli.Command, live bool) (scheduler.Config, error) {
	defaults := config.LoadUserDefaults(defaultsPath())
	sc := defaults.Scheduler

	pipelineCfg, err := config.LoadPipelineConfig(c.String("config"))
	if err != nil {
		return scheduler.Config{}, fmt.Errorf("loading pipeline config: %w", err)
	}
	alertCfg, err := config.LoadAlertConfig(c.String("config"))
	if err != nil {
		return scheduler.Config{}, fmt.Errorf("loading alert config: %w", err)
	}

	b, err := newBroker(live)
	if err != nil {
		return scheduler.Config{}, fmt.Errorf("creating broker: %w", err)
	}

	topN := defaults.TopN
	if topN == 0 {
		topN = 20
	}

	return scheduler.Config{
		Clock:           broker.TradingClock(),
		Broker:          b,
		Alert:           alertCfg,
		Pipeline:        *pipelineCfg,
		PortfolioFile:   resolvePortfolioFile(c, alertCfg),
		ConfigPath:      c.String("config"),
		EODIndex:        defaults.Index,
		EODMethod:       defaults.Method,
		EODTopN:         topN,
		CloseOffsetMin:  sc.CloseOffsetMin,
		EnableEOD:       sc.EnableEOD,
		EnableDrift:     sc.EnableDrift,
		EnableRebalance: sc.EnableRebalance,
		AutoExecute:     pipelineCfg.Schedule.AutoExecute,
		Live:            live,
	}, nil
}

func runScheduler(ctx context.Context, c *cli.Command) error {
	cfg, err := buildSchedulerConfig(c, c.Bool("live"))
	if err != nil {
		return err
	}
	fmt.Printf("Scheduler starting (EOD=%t drift=%t rebalance=%t, live=%t).\n",
		cfg.EnableEOD, cfg.EnableDrift, cfg.EnableRebalance, cfg.Live)
	return scheduler.New(cfg).Run(ctx)
}

func runSchedulerStatus(_ context.Context, _ *cli.Command) error {
	state, err := scheduler.LoadState()
	if err != nil {
		return fmt.Errorf("reading scheduler state: %w", err)
	}
	rows := []render.KVPair{
		{Key: "Last EOD", Value: lastRunOrNever(state, scheduler.CadenceEOD)},
		{Key: "Last drift check", Value: lastRunOrNever(state, scheduler.CadenceDrift)},
		{Key: "Last rebalance", Value: lastRunOrNever(state, scheduler.CadenceRebalance)},
	}
	render.KV(os.Stdout, rows)
	return nil
}

func lastRunOrNever(state scheduler.State, c scheduler.Cadence) string {
	if state.LastRun == nil || state.LastRun[string(c)] == "" {
		return "never"
	}
	return state.LastRun[string(c)]
}

// launchd plist for the scheduler: a keep-alive process (all cadence timing is
// in-process), mirroring the daemon plist it replaces.
var schedulerPlistTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.mycase.scheduler</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.BinaryPath}}</string>
		<string>scheduler</string>
		<string>run</string>
		<string>--live</string>
	</array>
	<key>WorkingDirectory</key>
	<string>{{.WorkDir}}</string>
	<key>KeepAlive</key>
	<true/>
	<key>RunAtLoad</key>
	<true/>
	<key>StandardOutPath</key>
	<string>{{.WorkDir}}/data/scheduler.log</string>
	<key>StandardErrorPath</key>
	<string>{{.WorkDir}}/data/scheduler.log</string>
</dict>
</plist>
`

const schedulerPlistPath = "com.mycase.scheduler.plist"

func runSchedulerInstall(_ context.Context, _ *cli.Command) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding binary path: %w", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	if runtime.GOOS != "darwin" {
		printSchedulerSystemdUnit(binPath, wd)
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		return fmt.Errorf("creating LaunchAgents dir: %w", err)
	}
	plistFile := filepath.Join(plistDir, schedulerPlistPath)

	f, err := os.Create(plistFile)
	if err != nil {
		return fmt.Errorf("creating plist file: %w", err)
	}
	defer f.Close()

	tmpl := template.Must(template.New("plist").Parse(schedulerPlistTmpl))
	if err := tmpl.Execute(f, plistData{BinaryPath: binPath, WorkDir: wd}); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	fmt.Printf("Installed: %s\n", plistFile)
	fmt.Printf("To load now: launchctl load %s\n", plistFile)
	fmt.Println("The scheduler owns the EOD, drift, and rebalance cadences — it replaces")
	fmt.Println("the separate 'daemon install' and 'autopilot install' units. If either of")
	fmt.Println("those is installed, uninstall it to avoid duplicate runs.")
	return nil
}

func runSchedulerUninstall(_ context.Context, _ *cli.Command) error {
	if runtime.GOOS != "darwin" {
		fmt.Println("Uninstall is only supported on macOS. Remove the systemd unit manually.")
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	plistFile := filepath.Join(home, "Library", "LaunchAgents", schedulerPlistPath)
	exec.Command("launchctl", "unload", plistFile).Run() //nolint:errcheck
	if err := os.Remove(plistFile); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No scheduler service installed.")
			return nil
		}
		return fmt.Errorf("removing plist: %w", err)
	}
	fmt.Printf("Removed %s\n", plistFile)
	return nil
}

func printSchedulerSystemdUnit(binPath, wd string) {
	fmt.Printf(`# Save as ~/.config/systemd/user/mycase-scheduler.service
[Unit]
Description=Mycase autonomous scheduler (EOD / drift / rebalance)
After=network.target

[Service]
Type=simple
ExecStart=%s scheduler run --live
WorkingDirectory=%s
Restart=on-failure
StandardOutput=append:%s/data/scheduler.log
StandardError=append:%s/data/scheduler.log

[Install]
WantedBy=default.target

# Enable and start:
#   systemctl --user enable mycase-scheduler
#   systemctl --user start  mycase-scheduler
`, binPath, wd, wd, wd)
}
