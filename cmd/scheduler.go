package cmd

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/marketcal"
	"github.com/raghavkgarg/mycase/pkg/render"
	"github.com/raghavkgarg/mycase/pkg/scheduler"
)

// SchedulerCommand is the autonomous orchestrator (Phase 12): it owns the daily
// EOD update, the daily drift check, and the quarterly/monthly rebalance proposal
// as one sequenced pass — replacing the separate `daemon install` and `autopilot
// install` units with a single OS timer. The installed model is a one-shot
// (`scheduler run-now` fired daily by launchd/systemd); `scheduler daemon` keeps
// the older keep-alive loop available for a future intraday-reactive case.
var SchedulerCommand = &cli.Command{
	Name:  "scheduler",
	Usage: "Autonomous orchestrator for EOD / drift / rebalance cadences",
	Commands: []*cli.Command{
		schedulerRunNowCmd,
		schedulerDaemonCmd,
		schedulerStatusCmd,
		schedulerInstallCmd,
		schedulerUninstallCmd,
	},
}

// schedulerRunNowCmd runs today's due work once and exits. This is what the OS
// timer invokes and what an operator runs to recover a missed day. It defaults to
// the live broker + real data (the only fully-meaningful mode); use --dry-run for
// a safe preview that touches nothing, or --mock for a non-live broker.
var schedulerRunNowCmd = &cli.Command{
	Name:  "run-now",
	Usage: "Run today's due cadences (catch-up + EOD/drift/rebalance) once and exit",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "dry-run", Usage: "Preview which cadences would run for today; fetch nothing, write nothing"},
		&cli.BoolFlag{Name: "mock", Usage: "Use the mock broker instead of the live API (drift/rebalance become non-live)"},
		&cli.StringFlag{Name: "config", Value: defaultPipelineConfig(), Usage: "Pipeline config file"},
		&cli.StringFlag{Name: "file", Usage: "Portfolio CSV for the drift check (overrides config)"},
	},
	Action: runSchedulerRunNow,
}

// schedulerDaemonCmd runs the resident keep-alive tick loop (blocks). Retained for
// a future intraday-reactive case; the installed timer uses `run-now` instead.
var schedulerDaemonCmd = &cli.Command{
	Name:  "daemon",
	Usage: "Run the resident keep-alive loop (blocks; for the intraday-reactive case — install uses run-now)",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "mock", Usage: "Use the mock broker instead of the live API"},
		&cli.StringFlag{Name: "config", Value: defaultPipelineConfig(), Usage: "Pipeline config file"},
		&cli.StringFlag{Name: "file", Usage: "Portfolio CSV for the drift check (overrides config)"},
	},
	Action: runSchedulerDaemon,
}

var schedulerStatusCmd = &cli.Command{
	Name:   "status",
	Usage:  "Show the last completed trading day per cadence and flag if behind",
	Action: runSchedulerStatus,
}

var schedulerInstallCmd = &cli.Command{
	Name:   "install",
	Usage:  "Install the daily OS timer (launchd on macOS; systemd unit+timer printed on Linux)",
	Action: runSchedulerInstall,
}

var schedulerUninstallCmd = &cli.Command{
	Name:   "uninstall",
	Usage:  "Remove the installed scheduler timer",
	Action: runSchedulerUninstall,
}

// defaultPipelineConfig returns the pipeline YAML the scheduler should use,
// sourced from defaults.json's pipeline_config so the market path (US vs India)
// is a single-file switch. Falls back to config/pipeline.yaml when unset.
func defaultPipelineConfig() string {
	defaults := config.LoadUserDefaults(config.Path("defaults.json"))
	if defaults.PipelineConfig != "" {
		return config.Path(filepath.Base(defaults.PipelineConfig))
	}
	return config.Path("pipeline.yaml")
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

func runSchedulerDaemon(ctx context.Context, c *cli.Command) error {
	cfg, err := buildSchedulerConfig(c, !c.Bool("mock"))
	if err != nil {
		return err
	}
	fmt.Printf("Scheduler daemon starting (EOD=%t drift=%t rebalance=%t, live=%t).\n",
		cfg.EnableEOD, cfg.EnableDrift, cfg.EnableRebalance, cfg.Live)
	return scheduler.New(cfg).Run(ctx)
}

// runSchedulerRunNow runs a single sequenced pass and exits. This is the entry
// point the installed OS timer (launchd StartCalendarInterval / systemd
// OnCalendar) invokes once per trading day at market-close+offset, and what an
// operator runs to recover a missed day. Live by default; --mock for a non-live
// broker; --dry-run to preview without touching anything.
func runSchedulerRunNow(ctx context.Context, c *cli.Command) error {
	dryRun := c.Bool("dry-run")
	// A dry-run inspects state + clock only; it must not require a live broker
	// (e.g. valid Schwab tokens), so force the mock broker for the preview.
	live := !c.Bool("mock") && !dryRun
	cfg, err := buildSchedulerConfig(c, live)
	if err != nil {
		return err
	}
	if dryRun {
		// Report the intended live/mock mode of a real run, not the forced-mock
		// preview broker, so the operator sees what run-now would actually use.
		cfg.Live = !c.Bool("mock")
		plan := scheduler.New(cfg).Plan(time.Now())
		fmt.Print(plan.Render())
		return nil
	}
	return scheduler.New(cfg).RunOnce(ctx)
}

func runSchedulerStatus(_ context.Context, _ *cli.Command) error {
	state, err := scheduler.LoadState()
	if err != nil {
		return fmt.Errorf("reading scheduler state: %w", err)
	}
	clk := broker.TradingClock()
	rows := []render.KVPair{
		{Key: "Last EOD", Value: lastRunOrNever(state, scheduler.CadenceEOD)},
		{Key: "Last drift check", Value: lastRunOrNever(state, scheduler.CadenceDrift)},
		{Key: "Last rebalance", Value: lastRunOrNever(state, scheduler.CadenceRebalance)},
	}
	render.KV(os.Stdout, rows)

	// Staleness: compare the EOD last-run against the most recent settled trading
	// day. If behind, tell the operator exactly how to recover — a fresh run-now
	// gets current (prices self-heal via the range fetch; we intentionally do not
	// backfill per-day PIT snapshots — see the runbook).
	behind := scheduler.TradingDaysBehind(state, clk, time.Now())
	if behind > 0 {
		last := lastRunOrNever(state, scheduler.CadenceEOD)
		expected := clk.SettlementDate(time.Now()).Format("2006-01-02")
		day := "day"
		if behind > 1 {
			day = "days"
		}
		fmt.Printf("\n⚠  EOD is %d trading %s behind (last: %s, latest settled: %s).\n",
			behind, day, last, expected)
		fmt.Println("   Recover with:  mycase scheduler run-now")
	} else {
		fmt.Println("\n✓  Up to date.")
	}
	return nil
}

func lastRunOrNever(state scheduler.State, c scheduler.Cadence) string {
	if state.LastRun == nil || state.LastRun[string(c)] == "" {
		return "never"
	}
	return state.LastRun[string(c)]
}

const (
	schedulerPlistLabel = "com.mycase.scheduler"
	schedulerPlistPath  = schedulerPlistLabel + ".plist"
)

//go:embed scheduler_plist.tmpl
var schedulerPlistTemplate string

// schedulerPlistData fills the committed plist template. Hour/Minute are in the
// LOCAL (machine) timezone — launchd's StartCalendarInterval fires on wall-clock
// local time — computed by converting the active market's close+offset into local
// time (see localFireTime).
type schedulerPlistData struct {
	BinaryPath string
	WorkDir    string
	Hour       int
	Minute     int
}

// localFireTime converts the active market's daily fire moment (close cutoff +
// offset, in the market's own timezone) into the machine's local wall-clock
// Hour:Minute, which is what launchd's StartCalendarInterval expects. Example:
// NYSE close 16:00 ET + 15m = 16:15 ET → 20:15 IST for a machine in Kolkata.
//
// It anchors on today's date so the conversion uses the DST offset currently in
// effect on both sides. A StartCalendarInterval is a fixed local wall-clock time,
// so when either zone crosses a DST boundary the installed fire time drifts by up
// to an hour until the next `scheduler install`; anchoring on "now" keeps it
// correct for the current season, which is the best a fixed local time can do.
func localFireTime(clk marketcal.Clock, offset time.Duration) (hour, minute int) {
	loc := clk.Loc
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now()
	// Anchor on today's date in the market zone; only the resulting clock time
	// matters for a daily calendar fire.
	base := time.Date(now.Year(), now.Month(), now.Day(), clk.CutoffHour, 0, 0, 0, loc).Add(offset)
	local := base.In(now.Location())
	return local.Hour(), local.Minute()
}

func runSchedulerInstall(_ context.Context, c *cli.Command) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding binary path: %w", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	clk := broker.TradingClock()
	offset := time.Duration(schedulerCloseOffsetMin()) * time.Minute
	hour, minute := localFireTime(clk, offset)

	if runtime.GOOS != "darwin" {
		printSchedulerSystemdUnit(binPath, wd, hour, minute)
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

	tmpl := template.Must(template.New("plist").Parse(schedulerPlistTemplate))
	data := schedulerPlistData{BinaryPath: binPath, WorkDir: wd, Hour: hour, Minute: minute}
	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	// Load (or reload) with the modern launchctl bootstrap API. bootout first so a
	// reinstall picks up a changed fire time; ignore its error (nothing loaded yet).
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	exec.Command("launchctl", "bootout", domain+"/"+schedulerPlistLabel).Run() //nolint:errcheck
	if out, bErr := exec.Command("launchctl", "bootstrap", domain, plistFile).CombinedOutput(); bErr != nil {
		fmt.Printf("Installed %s but launchctl bootstrap failed: %v\n%s\n", plistFile, bErr, out)
		fmt.Printf("Load it manually with: launchctl bootstrap %s %s\n", domain, plistFile)
		return nil
	}

	fmt.Printf("Installed and loaded: %s\n", plistFile)
	fmt.Printf("Fires daily at %02d:%02d local (%s close +%dm).\n",
		hour, minute, clk.Loc, schedulerCloseOffsetMin())
	fmt.Println("The scheduler owns the EOD, drift, and rebalance cadences — it replaces")
	fmt.Println("the separate 'daemon install' and 'autopilot install' units. If either of")
	fmt.Println("those is installed, uninstall it to avoid duplicate runs.")
	return nil
}

func runSchedulerUninstall(_ context.Context, _ *cli.Command) error {
	if runtime.GOOS != "darwin" {
		fmt.Println("Uninstall is only supported on macOS. Remove the systemd unit/timer manually.")
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	plistFile := filepath.Join(home, "Library", "LaunchAgents", schedulerPlistPath)
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	exec.Command("launchctl", "bootout", domain+"/"+schedulerPlistLabel).Run() //nolint:errcheck
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

// schedulerCloseOffsetMin returns the configured post-close offset (minutes),
// defaulting to 15 when unset — mirrors scheduler.Config.closeOffset().
func schedulerCloseOffsetMin() int {
	sc := config.LoadUserDefaults(defaultsPath()).Scheduler
	if sc.CloseOffsetMin <= 0 {
		return 15
	}
	return sc.CloseOffsetMin
}

func printSchedulerSystemdUnit(binPath, wd string, hour, minute int) {
	fmt.Printf(`# One-shot service + timer. Save the .service and .timer under
# ~/.config/systemd/user/ then: systemctl --user enable --now mycase-scheduler.timer

# ── mycase-scheduler.service ──
[Unit]
Description=Mycase autonomous scheduler tick (EOD / drift / rebalance)
After=network.target

[Service]
Type=oneshot
ExecStart=%s scheduler run-now
WorkingDirectory=%s
StandardOutput=append:%s/data/scheduler.log
StandardError=append:%s/data/scheduler.log

# ── mycase-scheduler.timer ──
[Unit]
Description=Fire the mycase scheduler tick daily at market close+offset

[Timer]
OnCalendar=*-*-* %02d:%02d:00
Persistent=true

[Install]
WantedBy=timers.target
`, binPath, wd, wd, wd, hour, minute)
}
