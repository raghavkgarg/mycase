package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"text/template"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/daemon"
	"github.com/raghavkgarg/mycase/pkg/render"
)

var DaemonCommand = &cli.Command{
	Name:  "daemon",
	Usage: "Background portfolio drift monitoring daemon",
	Commands: []*cli.Command{
		daemonStartCmd,
		daemonStopCmd,
		daemonStatusCmd,
		daemonCheckCmd,
		daemonInstallCmd,
		daemonUninstallCmd,
	},
}

var daemonStartCmd = &cli.Command{
	Name:  "start",
	Usage: "Start the drift monitoring loop (blocks until stopped; use install for system service)",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "live", Usage: "Use live broker API (default: mock)"},
		&cli.StringFlag{Name: "config", Value: "config/pipeline.yaml", Usage: "Pipeline config file"},
		&cli.StringFlag{Name: "file", Usage: "Portfolio CSV to check drift against (overrides config)"},
	},
	Action: runDaemonStart,
}

var daemonStopCmd = &cli.Command{
	Name:   "stop",
	Usage:  "Send SIGTERM to the running daemon",
	Action: runDaemonStop,
}

var daemonStatusCmd = &cli.Command{
	Name:   "status",
	Usage:  "Show last drift check results from state file",
	Action: runDaemonStatus,
}

var daemonCheckCmd = &cli.Command{
	Name:  "check",
	Usage: "Run a one-shot drift check and exit",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "live", Usage: "Use live broker API (default: mock)"},
		&cli.StringFlag{Name: "config", Value: "config/pipeline.yaml", Usage: "Pipeline config file"},
		&cli.StringFlag{Name: "file", Usage: "Portfolio CSV to check drift against (overrides config)"},
	},
	Action: runDaemonCheck,
}

var daemonInstallCmd = &cli.Command{
	Name:   "install",
	Usage:  "Install system service (launchd on macOS; prints systemd unit on Linux)",
	Action: runDaemonInstall,
}

var daemonUninstallCmd = &cli.Command{
	Name:   "uninstall",
	Usage:  "Remove installed system service",
	Action: runDaemonUninstall,
}

func resolvePortfolioFile(c *cli.Command, alertCfg config.AlertConfig) string {
	if f := c.String("file"); f != "" {
		return f
	}
	if alertCfg.PortfolioFile != "" {
		return alertCfg.PortfolioFile
	}
	return "data/microsmall.csv"
}

func runDaemonStart(ctx context.Context, c *cli.Command) error {
	alertCfg, err := config.LoadAlertConfig(c.String("config"))
	if err != nil {
		return err
	}
	portfolioFile := resolvePortfolioFile(c, alertCfg)
	b, err := newBroker(c.Bool("live"))
	if err != nil {
		return fmt.Errorf("creating broker: %w", err)
	}
	fmt.Printf("Drift daemon starting. Portfolio: %s, threshold: %.1f%%\n",
		portfolioFile, alertCfg.DriftThreshold*100)
	return daemon.RunLoop(ctx, b, alertCfg, portfolioFile)
}

func runDaemonStop(_ context.Context, _ *cli.Command) error {
	data, err := os.ReadFile(daemon.PIDFile)
	if err != nil {
		return fmt.Errorf("no PID file at %s — is the daemon running?", daemon.PIDFile)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("invalid PID file: %w", err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("could not signal process %d: %w", pid, err)
	}
	fmt.Printf("Sent SIGTERM to daemon (PID %d)\n", pid)
	return nil
}

func runDaemonStatus(_ context.Context, _ *cli.Command) error {
	state, err := daemon.LoadState()
	if err != nil {
		return fmt.Errorf("reading state: %w", err)
	}
	if state.LastCheckAt.IsZero() {
		fmt.Println("No drift checks have run yet.")
		return nil
	}
	level := "OK"
	if state.LastDrift > 0.10 {
		level = "CRITICAL"
	} else if state.LastDrift > 0.05 {
		level = "WARN"
	}
	render.KV(os.Stdout, []render.KVPair{
		{Key: "Last check", Value: state.LastCheckAt.Local().Format("2006-01-02 15:04:05 MST")},
		{Key: "Drift index", Value: fmt.Sprintf("%.4f  [%s]", state.LastDrift, level)},
		{Key: "Portfolio", Value: state.PortfolioFile},
		{Key: "Alerts sent", Value: fmt.Sprintf("%d", state.AlertsSent)},
	})
	return nil
}

func runDaemonCheck(ctx context.Context, c *cli.Command) error {
	alertCfg, err := config.LoadAlertConfig(c.String("config"))
	if err != nil {
		return err
	}
	portfolioFile := resolvePortfolioFile(c, alertCfg)
	b, err := newBroker(c.Bool("live"))
	if err != nil {
		return fmt.Errorf("creating broker: %w", err)
	}

	result, err := daemon.RunCheck(ctx, b, alertCfg, portfolioFile)
	if err != nil {
		return err
	}

	mktCfg := broker.LoadMarketConfig()
	fmt.Printf("Drift check at %s\n", result.CheckedAt.Local().Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Portfolio:     %s\n", portfolioFile)
	fmt.Printf("Total value:   %s%.2f\n", mktCfg.Currency, result.TotalValue)
	fmt.Printf("Drift index:   %.4f (%.1f%%)\n", result.DriftIndex, result.DriftIndex*100)
	fmt.Printf("Threshold:     %.4f (%.1f%%)\n", alertCfg.DriftThreshold, alertCfg.DriftThreshold*100)

	if result.DriftIndex > alertCfg.DriftThreshold {
		fmt.Println("\n[DRIFT ALERT] Portfolio has drifted beyond threshold — consider rebalancing.")
	} else {
		fmt.Println("\n[OK] Portfolio drift within acceptable range.")
	}

	fmt.Printf("\n  %-22s %9s %9s %9s\n", "Instrument", "Target", "Actual", "Diff")
	fmt.Printf("  %-22s %9s %9s %9s\n",
		strings.Repeat("-", 22), strings.Repeat("-", 9), strings.Repeat("-", 9), strings.Repeat("-", 9))

	for _, key := range result.BasketKeys {
		target := result.TargetWeights[key]
		actual := result.ActualWeights[key]
		diff := actual - target
		fmt.Printf("  %-22s %8.2f%% %8.2f%% %+8.2f%%\n", key, target*100, actual*100, diff*100)
	}
	return nil
}

// launchd plist template for macOS. StartCalendarInterval uses local time.
var launchdPlistTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.mycase.daemon</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.BinaryPath}}</string>
		<string>daemon</string>
		<string>start</string>
		<string>--live</string>
	</array>
	<key>WorkingDirectory</key>
	<string>{{.WorkDir}}</string>
	<key>KeepAlive</key>
	<true/>
	<key>RunAtLoad</key>
	<true/>
	<key>StandardOutPath</key>
	<string>{{.WorkDir}}/data/daemon.log</string>
	<key>StandardErrorPath</key>
	<string>{{.WorkDir}}/data/daemon.log</string>
</dict>
</plist>
`

type plistData struct {
	BinaryPath string
	WorkDir    string
}

const launchdPlistPath = "com.mycase.daemon.plist"

func runDaemonInstall(_ context.Context, _ *cli.Command) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding binary path: %w", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	if runtime.GOOS != "darwin" {
		printSystemdUnit(binPath, wd)
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents dir: %w", err)
	}
	plistFile := filepath.Join(plistDir, launchdPlistPath)

	f, err := os.Create(plistFile)
	if err != nil {
		return fmt.Errorf("creating plist file: %w", err)
	}
	defer f.Close()

	tmpl := template.Must(template.New("plist").Parse(launchdPlistTmpl))
	if err := tmpl.Execute(f, plistData{BinaryPath: binPath, WorkDir: wd}); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	fmt.Printf("Installed: %s\n", plistFile)
	fmt.Printf("To load now: launchctl load %s\n", plistFile)
	fmt.Println("The daemon will also start automatically at next login.")
	return nil
}

func runDaemonUninstall(_ context.Context, _ *cli.Command) error {
	if runtime.GOOS != "darwin" {
		fmt.Println("Uninstall is only supported on macOS. Remove the systemd unit manually.")
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	plistFile := filepath.Join(home, "Library", "LaunchAgents", launchdPlistPath)
	// Best-effort unload before removing.
	exec.Command("launchctl", "unload", plistFile).Run() //nolint:errcheck
	if err := os.Remove(plistFile); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No service installed.")
			return nil
		}
		return fmt.Errorf("removing plist: %w", err)
	}
	fmt.Printf("Removed %s\n", plistFile)
	return nil
}

func printSystemdUnit(binPath, wd string) {
	fmt.Printf(`# Save as ~/.config/systemd/user/mycase-daemon.service
[Unit]
Description=Mycase drift monitoring daemon
After=network.target

[Service]
Type=simple
ExecStart=%s daemon start --live
WorkingDirectory=%s
Restart=on-failure
StandardOutput=append:%s/data/daemon.log
StandardError=append:%s/data/daemon.log

[Install]
WantedBy=default.target

# Enable and start:
#   systemctl --user enable mycase-daemon
#   systemctl --user start  mycase-daemon
`, binPath, wd, wd, wd)
}
