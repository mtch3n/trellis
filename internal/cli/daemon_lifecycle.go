package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/service"
	"github.com/spf13/cobra"
)

func newDaemonInstallCmd() *cobra.Command {
	var port int
	var bind string
	var linger, noStart bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the daemon as a login service so it starts automatically",
		Long: "Write a systemd user unit (Linux) or LaunchAgent (macOS) that starts the\n" +
			"Trellis daemon at login, then start it. Port and bind default to the ui.*\n" +
			"settings in the config file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager := service.New()
			bind, port := daemonDefaults(bind, port)
			spec, err := daemonSpec(bind, port, linger)
			if err != nil {
				return err
			}
			// A self-managed daemon already holds the database lock, so the
			// service would fail to start on top of it.
			status, err := resolveDaemonStatus(cmd.Context())
			if err != nil {
				return err
			}
			if status.ManagedBy == managedBySelf {
				if err := stopSelfManaged(cmd.Context(), status.Root); err != nil {
					return err
				}
			}
			if err := manager.Install(spec); err != nil {
				return installError(err)
			}
			path, _ := manager.UnitPath()
			if noStart {
				// Install always enables the unit; stopping it again is the
				// only way to honour --no-start without a separate code path.
				_ = manager.Stop()
			} else if err := waitForDaemon(cmd.Context(), status.Root, true); err != nil {
				return err
			}
			url, _ := daemonHealth(cmd.Context(), status.Root)
			return Emit(cmd, map[string]any{
				"installed": true, "manager": manager.Name(), "unit_path": path,
				"exec": spec.Exec, "bind": bind, "port": port, "linger": linger, "url": url,
			}, func() string {
				var b strings.Builder
				fmt.Fprintf(&b, "installed trellis daemon as a %s service\n", manager.Name())
				fmt.Fprintf(&b, "  unit: %s\n", path)
				fmt.Fprintf(&b, "  exec: %s daemon --bind %s --port %d\n", spec.Exec, bind, port)
				if noStart {
					b.WriteString("  not started (--no-start); run `trellis daemon start`")
					return b.String()
				}
				fmt.Fprintf(&b, "  running at %s", url)
				return b.String()
			})
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "HTTP port (default: ui.port)")
	cmd.Flags().StringVar(&bind, "bind", "", "HTTP bind address (default: ui.bind)")
	cmd.Flags().BoolVar(&linger, "linger", false, "Linux: keep the daemon running when you are not logged in")
	cmd.Flags().BoolVar(&noStart, "no-start", false, "install the service without starting it now")
	return cmd
}

func newDaemonUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the daemon service and remove its unit file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager := service.New()
			path, _ := manager.UnitPath()
			if err := manager.Uninstall(); err != nil {
				if errors.Is(err, service.ErrNotInstalled) {
					return core.ErrUsage("not_installed",
						"the trellis daemon is not installed as a service",
						"trellis daemon install")
				}
				return installError(err)
			}
			return Emit(cmd, map[string]any{"installed": false, "unit_path": path}, func() string {
				return "removed " + path
			})
		},
	}
}

func newDaemonStartCmd() *cobra.Command {
	var port int
	var bind string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon in the background",
		Long: "Start the daemon through the installed service manager when `daemon install`\n" +
			"has been run, and otherwise as a detached background process.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := resolveDaemonStatus(cmd.Context())
			if err != nil {
				return err
			}
			if status.Running {
				return Emit(cmd, map[string]any{"running": true, "already": true, "url": status.URL, "managed_by": status.ManagedBy},
					func() string { return "trellis daemon is already running at " + status.URL })
			}
			if status.Service.Installed {
				if err := service.New().Start(); err != nil {
					return installError(err)
				}
				if err := waitForDaemon(cmd.Context(), status.Root, true); err != nil {
					return err
				}
				url, _ := daemonHealth(cmd.Context(), status.Root)
				return Emit(cmd, map[string]any{"running": true, "url": url, "managed_by": managedByService},
					func() string { return "started trellis daemon via " + service.New().Name() + " at " + url })
			}
			bind, port := daemonDefaults(bind, port)
			pid, err := spawnDaemon(cmd.Context(), status.Root, bind, port)
			if err != nil {
				return err
			}
			url, _ := daemonHealth(cmd.Context(), status.Root)
			return Emit(cmd, map[string]any{"running": true, "pid": pid, "url": url, "managed_by": managedBySelf},
				func() string {
					return fmt.Sprintf("started trellis daemon (pid %d) at %s\n"+
						"  run `trellis daemon install` to start it automatically at login", pid, url)
				})
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "HTTP port (default: ui.port)")
	cmd.Flags().StringVar(&bind, "bind", "", "HTTP bind address (default: ui.bind)")
	return cmd
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := resolveDaemonStatus(cmd.Context())
			if err != nil {
				return err
			}
			switch status.ManagedBy {
			case managedByService:
				if !status.Running {
					return core.ErrUsage("not_running", "the trellis daemon is not running", "trellis daemon start")
				}
				if err := service.New().Stop(); err != nil {
					return installError(err)
				}
				if err := waitForDaemon(cmd.Context(), status.Root, false); err != nil {
					return err
				}
			case managedBySelf:
				if err := stopSelfManaged(cmd.Context(), status.Root); err != nil {
					return err
				}
			case managedByForeign:
				return core.ErrUsage("foreign_daemon",
					"a trellis daemon is running that this command did not start (a foreground `trellis daemon`, most likely)",
					"stop it where it is running, or run `trellis doctor`")
			default:
				return core.ErrUsage("not_running", "the trellis daemon is not running", "trellis daemon start")
			}
			return Emit(cmd, map[string]any{"running": false}, func() string { return "stopped trellis daemon" })
		},
	}
}

func newDaemonRestartCmd() *cobra.Command {
	var port int
	var bind string
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := resolveDaemonStatus(cmd.Context())
			if err != nil {
				return err
			}
			if status.ManagedBy == managedByForeign {
				return core.ErrUsage("foreign_daemon",
					"a trellis daemon is running that this command did not start",
					"stop it where it is running, or run `trellis doctor`")
			}
			if status.Service.Installed {
				if err := service.New().Restart(); err != nil {
					return installError(err)
				}
			} else {
				if status.ManagedBy == managedBySelf {
					if err := stopSelfManaged(cmd.Context(), status.Root); err != nil {
						return err
					}
				}
				bind, port := daemonDefaults(bind, port)
				if _, err := spawnDaemon(cmd.Context(), status.Root, bind, port); err != nil {
					return err
				}
			}
			if err := waitForDaemon(cmd.Context(), status.Root, true); err != nil {
				return err
			}
			url, _ := daemonHealth(cmd.Context(), status.Root)
			return Emit(cmd, map[string]any{"running": true, "url": url}, func() string {
				return "restarted trellis daemon " + servingDescription(url)
			})
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "HTTP port (default: ui.port)")
	cmd.Flags().StringVar(&bind, "bind", "", "HTTP bind address (default: ui.bind)")
	return cmd
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the daemon is running and what supervises it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := resolveDaemonStatus(cmd.Context())
			if err != nil {
				return err
			}
			return Emit(cmd, status, func() string { return daemonStatusTable(status) })
		},
	}
}

func daemonStatusTable(status daemonStatus) string {
	var b strings.Builder
	if status.Running {
		b.WriteString("running   yes\n")
		if status.URL != "" {
			fmt.Fprintf(&b, "url       %s\n", status.URL)
		} else {
			b.WriteString("url       none (ui.enabled is false; IPC only)\n")
		}
	} else {
		b.WriteString("running   no\n")
	}
	fmt.Fprintf(&b, "managed   %s\n", describeManager(status))
	if status.PID > 0 {
		fmt.Fprintf(&b, "pid       %d\n", status.PID)
	}
	if status.Service.Installed {
		fmt.Fprintf(&b, "unit      %s\n", status.Service.UnitPath)
		fmt.Fprintf(&b, "at login  %s", yesNo(status.Service.Enabled))
		return b.String()
	}
	b.WriteString("at login  no (run `trellis daemon install`)")
	return b.String()
}

// servingDescription renders where a daemon is reachable. An IPC-only daemon
// has no URL, and "at " with nothing after it reads as a bug.
func servingDescription(url string) string {
	if url == "" {
		return "(local IPC only; ui.enabled is false)"
	}
	return "at " + url
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func describeManager(status daemonStatus) string {
	switch status.ManagedBy {
	case managedByService:
		return status.Service.ManagedBy
	case managedBySelf:
		return "background process started by the CLI"
	case managedByForeign:
		return "unknown (running, but no unit file or pidfile explains it)"
	default:
		return "nothing"
	}
}

// installError turns a service-manager failure into the CLI's structured
// error shape so `--json` callers see a code and a fix, not a bare string.
func installError(err error) error {
	if errors.Is(err, service.ErrUnsupported) {
		return core.ErrUsage("unsupported_platform", err.Error(), "trellis daemon start")
	}
	if errors.Is(err, service.ErrNotInstalled) {
		return core.ErrUsage("not_installed",
			"the trellis daemon is not installed as a service", "trellis daemon install")
	}
	return err
}
