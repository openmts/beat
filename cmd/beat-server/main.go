package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"beat/internal/auth"
	"beat/internal/config"
	"beat/internal/platform/sqlite"
	"beat/internal/server"
	"beat/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Printf("beat-server %s (%s, %s)\n", version.Version, version.Commit, version.Date)
			return
		case "help", "--help", "-h":
			usage()
			return
		case "reset-admin":
			cfg, err := config.LoadResetAdmin(os.Args[2:])
			if err != nil {
				fmt.Fprintln(os.Stderr, "configuration error:", err)
				usage()
				os.Exit(2)
			}
			resetAdmin(cfg)
			return
		case "install":
			if err := server.Install(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "Server installation failed:", err)
				os.Exit(1)
			}
			return
		}
	}
	cfg, err := config.LoadServer(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		usage()
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	app, err := server.New(ctx, cfg, logger)
	if err != nil {
		logger.Error("beat_server_startup_failed", "error", err)
		os.Exit(1)
	}
	if err := app.Run(ctx); err != nil {
		logger.Error("beat_server_stopped", "error", err)
		os.Exit(1)
	}
}

func resetAdmin(cfg config.Config) {
	ctx := context.Background()
	store, err := sqlite.Open(cfg.DatabasePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database:", err)
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "prepare database:", err)
		os.Exit(1)
	}
	if _, err := auth.ResetCredentials(ctx, store); err != nil {
		fmt.Fprintln(os.Stderr, "reset admin credentials:", err)
		os.Exit(1)
	}
	fmt.Printf("Admin credentials reset: username=%s password=%s\n", auth.DefaultUsername, auth.DefaultPassword)
	fmt.Println("All existing admin sessions will be invalidated on their next request.")
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  beat-server [flags]     Start the Beat server
  beat-server version     Print version information
  beat-server install [--no-start]
                          Install the Server, Agent packages and systemd unit
  beat-server reset-admin [--config PATH] [--data-dir PATH]
                          Reset the single admin account to admin/admin

Server flags:
  --config PATH                 YAML configuration file
  --listen ADDRESS              HTTP + gRPC listen address (default [::]:9099)
  --data-dir PATH               data directory (default ./data)
  --agent-binary-dir PATH       directory containing beat-agent binaries
  --agent-token TOKEN           optional fixed token, at least 32 characters
  --enrollment-token-ttl VALUE  enrollment token lifetime (default 10m, range 1m-24h)
  --tls-cert PATH               TLS certificate path; requires --tls-key
  --tls-key PATH                TLS private key path; requires --tls-cert
  --demo                        seed and refresh demonstration data

Environment variables remain supported for service-manager compatibility.
Command-line flags take precedence.`)
}
