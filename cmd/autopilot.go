package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"
	"time"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/raghavkgarg/mycase/pkg/autopilot"
	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
)

var AutopilotCommand = &cli.Command{
	Name:  "autopilot",
	Usage: "Scheduled non-interactive rebalance pipeline (quarterly/monthly)",
	Commands: []*cli.Command{
		autopilotRunCmd,
		autopilotStatusCmd,
		autopilotDismissCmd,
		autopilotInstallCmd,
		autopilotUninstallCmd,
	},
}

var autopilotRunCmd = &cli.Command{
	Name:  "run",
	Usage: "Execute the non-interactive pipeline: pick → optimize → propose orders → alert",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "config", Value: "config/pipeline.yaml", Usage: "Pipeline config file"},
		&cli.BoolFlag{Name: "live", Usage: "Use live broker for holdings/quotes (default: mock)"},
		&cli.BoolFlag{Name: "skip-trading-day-check", Usage: "Run even if today is not a trading day"},
	},
	Action: runAutopilotRun,
}

var autopilotStatusCmd = &cli.Command{
	Name:  "status",
	Usage: "Show pending proposal status and next scheduled run",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "config", Value: "config/pipeline.yaml", Usage: "Pipeline config file"},
	},
	Action: runAutopilotStatus,
}

var autopilotDismissCmd = &cli.Command{
	Name:   "dismiss",
	Usage:  "Dismiss the pending proposal without executing orders",
	Action: runAutopilotDismiss,
}

var autopilotInstallCmd = &cli.Command{
	Name:  "install",
	Usage: "Install system service for scheduled autopilot (launchd on macOS, systemd on Linux)",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "config", Value: "config/pipeline.yaml", Usage: "Pipeline config file"},
	},
	Action: runAutopilotInstall,
}

var autopilotUninstallCmd = &cli.Command{
	Name:   "uninstall",
	Usage:  "Remove the installed autopilot system service",
	Action: runAutopilotUninstall,
}

func runAutopilotRun(ctx context.Context, c *cli.Command) error {
	configPath := c.String("config")
	live := c.Bool("live")
	skipTradingDayCheck := c.Bool("skip-trading-day-check")

	fmt.Println("====================================================================")
	fmt.Println("             Mycase Autopilot — Non-Interactive Pipeline             ")
	fmt.Println("====================================================================")

	// Check trading day
	if !skipTradingDayCheck {
		if !autopilot.IsTradingDay(ctx) {
			fmt.Println("[autopilot] Today is not a trading day. Use --skip-trading-day-check to override.")
			fmt.Println("[autopilot] Will retry on next trading day.")
			return nil
		}
	}

	// Load config
	var cfg config.PipelineConfig
	configFile, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("opening config file %s: %w", configPath, err)
	}
	defer configFile.Close()
	if err := yaml.NewDecoder(configFile).Decode(&cfg); err != nil {
		return fmt.Errorf("parsing config file %s: %w", configPath, err)
	}

	// Load alert config
	alertCfg, err := config.LoadAlertConfig(configPath)
	if err != nil {
		fmt.Printf("[autopilot] Warning: could not load alert config: %v\n", err)
	}

	// Create broker
	b, err := newBroker(live)
	if err != nil {
		return fmt.Errorf("creating broker: %w", err)
	}

	// Run autopilot
	rc := autopilot.RunConfig{
		PipelineCfg: cfg,
		Broker:      b,
		ConfigPath:  configPath,
	}

	result, err := autopilot.Run(ctx, rc)
	if err != nil {
		return fmt.Errorf("autopilot run failed: %w", err)
	}

	// Send alerts
	if len(cfg.Schedule.Notify) > 0 {
		fmt.Printf("[autopilot] Sending alerts to: %v\n", cfg.Schedule.Notify)
		if err := autopilot.SendProposalAlerts(result.Proposal, cfg.Schedule, alertCfg); err != nil {
			fmt.Printf("[autopilot] Warning: alert delivery issue: %v\n", err)
		} else {
			fmt.Println("[autopilot] Alerts sent successfully.")
		}

		// Trailing-alpha nudge: if the strategy is materially lagging the
		// benchmark, suggest a review. Best-effort — never abort the run.
		assessment, benchmark, aerr := autopilot.AssessPortfolioAlpha(ctx, newDataRouter(), result.Proposal.Portfolio)
		if aerr != nil {
			fmt.Printf("[autopilot] Trailing-alpha check skipped: %v\n", aerr)
		} else if assessment.Nudge {
			fmt.Printf("[autopilot] Trailing alpha %+.2f%% ≤ %+.2f%% — sending strategy-review nudge.\n",
				assessment.Alpha*100, assessment.Threshold*100)
			if err := autopilot.SendAlphaNudgeAlerts(result.Proposal.Portfolio, benchmark, assessment, cfg.Schedule, alertCfg); err != nil {
				fmt.Printf("[autopilot] Warning: nudge delivery issue: %v\n", err)
			}
		}
	}

	// Print summary
	fmt.Println("\n====================================================================")
	fmt.Println("                      Autopilot Run Complete                        ")
	fmt.Println("====================================================================")
	fmt.Printf("Proposal ID:    %s\n", result.Proposal.ID)
	fmt.Printf("Portfolio:      %s\n", result.Proposal.Portfolio)
	fmt.Printf("Strategy:       %s\n", result.Proposal.Strategy)
	fmt.Printf("Entries:        %d new stocks\n", len(result.Proposal.Entries))
	mktCfg := broker.LoadMarketConfig()
	fmt.Printf("Exits:          %d removed\n", len(result.Proposal.Exits))
	fmt.Printf("Orders:         %d (%d filtered)\n", len(result.Proposal.Orders), len(result.Proposal.FilteredOut))
	fmt.Printf("Est. cost:      %s%.0f\n", mktCfg.Currency, result.Proposal.EstimatedCost)
	fmt.Printf("Expires:        %s\n", result.Proposal.ExpiresAt.Format("2006-01-02 15:04 MST"))
	fmt.Printf("\nConfirm via:    mycase serve → http://localhost:8080/#/rebalance\n")
	fmt.Printf("Or dismiss:     mycase autopilot dismiss\n")

	return nil
}

func runAutopilotStatus(ctx context.Context, c *cli.Command) error {
	configPath := c.String("config")

	proposal, err := autopilot.LoadProposal()
	if err != nil {
		return fmt.Errorf("loading proposal: %w", err)
	}

	if proposal == nil {
		fmt.Println("No pending proposal.")
	} else {
		fmt.Printf("Proposal ID:    %s\n", proposal.ID)
		fmt.Printf("Status:         %s\n", proposal.Status)
		fmt.Printf("Created:        %s\n", proposal.CreatedAt.Format("2006-01-02 15:04 MST"))
		fmt.Printf("Expires:        %s\n", proposal.ExpiresAt.Format("2006-01-02 15:04 MST"))
		fmt.Printf("Portfolio:      %s\n", proposal.Portfolio)
		fmt.Printf("Strategy:       %s\n", proposal.Strategy)
		fmt.Printf("Entries:        %d\n", len(proposal.Entries))
		fmt.Printf("Exits:          %d\n", len(proposal.Exits))
		fmt.Printf("Orders:         %d\n", len(proposal.Orders))
		fmt.Printf("Est. cost:      %s%.0f\n", broker.LoadMarketConfig().Currency, proposal.EstimatedCost)

		if proposal.IsExpired() {
			fmt.Println("\n⚠️  This proposal has expired and cannot be confirmed.")
		} else if proposal.Status == autopilot.StatusPending {
			remaining := time.Until(proposal.ExpiresAt)
			fmt.Printf("\n⏳ Expires in %.0f hours. Confirm via dashboard or dismiss.\n", remaining.Hours())
		}
	}

	// Show next scheduled run
	var cfg config.PipelineConfig
	if f, err := os.Open(configPath); err == nil {
		defer f.Close()
		_ = yaml.NewDecoder(f).Decode(&cfg)
	}

	if cfg.Schedule.Frequency != "" && cfg.Schedule.Frequency != "drift-triggered" {
		nextRun := autopilot.NextRunDate(cfg.Schedule)
		if !nextRun.IsZero() {
			fmt.Printf("\nNext scheduled run: %s (%s)\n", nextRun.Format("2006-01-02 15:04 MST"), cfg.Schedule.Frequency)
		}
	}

	return nil
}

func runAutopilotDismiss(_ context.Context, _ *cli.Command) error {
	proposal, err := autopilot.LoadProposal()
	if err != nil {
		return fmt.Errorf("loading proposal: %w", err)
	}
	if proposal == nil {
		fmt.Println("No pending proposal to dismiss.")
		return nil
	}
	if proposal.Status != autopilot.StatusPending {
		fmt.Printf("Proposal is already %s — cannot dismiss.\n", proposal.Status)
		return nil
	}

	if err := autopilot.DismissProposal(proposal); err != nil {
		return fmt.Errorf("dismissing proposal: %w", err)
	}

	// Archive it
	if err := autopilot.ArchiveProposal(proposal); err != nil {
		fmt.Printf("Warning: could not archive proposal: %v\n", err)
	}

	fmt.Printf("Proposal %s dismissed. No orders will be placed.\n", proposal.ID)
	return nil
}

// Launchd plist template for autopilot scheduling.
var autopilotPlistTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.mycase.autopilot</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.BinaryPath}}</string>
		<string>autopilot</string>
		<string>run</string>
		<string>--live</string>
		<string>--config</string>
		<string>{{.ConfigPath}}</string>
	</array>
	<key>WorkingDirectory</key>
	<string>{{.WorkDir}}</string>
	{{.ScheduleInterval}}
	<key>StandardOutPath</key>
	<string>{{.WorkDir}}/data/autopilot/autopilot.log</string>
	<key>StandardErrorPath</key>
	<string>{{.WorkDir}}/data/autopilot/autopilot.log</string>
</dict>
</plist>
`

type autopilotPlistData struct {
	BinaryPath       string
	WorkDir          string
	ConfigPath       string
	ScheduleInterval string
}

const autopilotPlistName = "com.mycase.autopilot.plist"

func runAutopilotInstall(_ context.Context, c *cli.Command) error {
	configPath := c.String("config")

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding binary path: %w", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	// Load config to determine frequency
	var cfg config.PipelineConfig
	if f, err := os.Open(configPath); err == nil {
		defer f.Close()
		_ = yaml.NewDecoder(f).Decode(&cfg)
	}

	if runtime.GOOS != "darwin" {
		printAutopilotSystemdUnit(binPath, wd, configPath, cfg.Schedule)
		return nil
	}

	// Determine schedule interval XML
	var scheduleInterval string
	switch cfg.Schedule.Frequency {
	case "monthly":
		day := 2
		if _, err := fmt.Sscanf(cfg.Schedule.Day, "%d", &day); err != nil || day < 1 || day > 28 {
			day = 2
		}
		scheduleInterval = autopilot.LaunchdMonthlyInterval(day)
	default: // quarterly
		scheduleInterval = autopilot.LaunchdQuarterlyIntervals()
	}

	// Ensure log directory exists
	_ = os.MkdirAll(filepath.Join(wd, "data", "autopilot"), 0755)

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents dir: %w", err)
	}
	plistFile := filepath.Join(plistDir, autopilotPlistName)

	f, err := os.Create(plistFile)
	if err != nil {
		return fmt.Errorf("creating plist file: %w", err)
	}
	defer f.Close()

	tmpl := template.Must(template.New("plist").Parse(autopilotPlistTmpl))
	data := autopilotPlistData{
		BinaryPath:       binPath,
		WorkDir:          wd,
		ConfigPath:       filepath.Join(wd, configPath),
		ScheduleInterval: scheduleInterval,
	}
	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	fmt.Printf("Installed: %s\n", plistFile)
	fmt.Printf("Frequency: %s\n", cfg.Schedule.Frequency)
	fmt.Printf("\nTo load now:\n  launchctl load %s\n", plistFile)
	fmt.Println("\nThe autopilot will fire on schedule. Each run generates a proposal and sends alerts.")
	fmt.Println("Confirm execution via the web dashboard or dismiss via `mycase autopilot dismiss`.")
	return nil
}

func runAutopilotUninstall(_ context.Context, _ *cli.Command) error {
	if runtime.GOOS != "darwin" {
		fmt.Println("Uninstall is only supported on macOS. Remove the systemd unit manually:")
		fmt.Println("  systemctl --user disable mycase-autopilot")
		fmt.Println("  rm ~/.config/systemd/user/mycase-autopilot.service")
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	plistFile := filepath.Join(home, "Library", "LaunchAgents", autopilotPlistName)
	// Best-effort unload before removing
	exec.Command("launchctl", "unload", plistFile).Run() //nolint:errcheck
	if err := os.Remove(plistFile); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No autopilot service installed.")
			return nil
		}
		return fmt.Errorf("removing plist: %w", err)
	}
	fmt.Printf("Removed %s\n", plistFile)
	return nil
}

func printAutopilotSystemdUnit(binPath, wd, configPath string, schedule config.ScheduleConfig) {
	// Determine OnCalendar expression
	var calendar string
	switch schedule.Frequency {
	case "monthly":
		day := "2"
		if schedule.Day != "" && schedule.Day != "first_trading_day" && schedule.Day != "last_trading_day" {
			day = schedule.Day
		}
		calendar = fmt.Sprintf("*-*-%s 10:00:00", day)
	default: // quarterly
		calendar = "*-01,04,07,10-02 10:00:00"
	}

	fmt.Printf(`# Save as ~/.config/systemd/user/mycase-autopilot.service
[Unit]
Description=Mycase autopilot quarterly rebalance
After=network.target

[Service]
Type=oneshot
ExecStart=%s autopilot run --live --config %s
WorkingDirectory=%s
StandardOutput=append:%s/data/autopilot/autopilot.log
StandardError=append:%s/data/autopilot/autopilot.log

[Install]
WantedBy=default.target

# Also create a timer: ~/.config/systemd/user/mycase-autopilot.timer
# [Unit]
# Description=Mycase autopilot schedule
#
# [Timer]
# OnCalendar=%s
# Persistent=true
#
# [Install]
# WantedBy=timers.target
#
# Enable:
#   systemctl --user enable --now mycase-autopilot.timer
`, binPath, filepath.Join(wd, configPath), wd, wd, wd, calendar)
}
