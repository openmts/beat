package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/beat/backend/internal/buildinfo"
)

func main() {
	configureLogger()
	if handled, err := handleRecoveryCommand(os.Args[1:], os.Stdout); handled {
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "operation=admin_recovery phase=complete result=failed code=recovery_rejected: %v\n", err)
			os.Exit(1)
		}
		_, _ = fmt.Fprintln(os.Stderr, "operation=admin_recovery phase=complete result=success code=ok")
		return
	}
	if handled, err := handleSecretCommand(os.Args[1:], os.Stdout); handled {
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	dbPath := flag.String("db-path", "./data/beat.db", "path to sqlite database file")
	mtsPath := flag.String("mts-path", "./data/beat_mts", "path to MTS time series data")
	listenAddr := flag.String("listen-addr", ":8080", "address to listen on")
	staticDir := flag.String("static-dir", "../webui/dist", "path to frontend static files")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		_, _ = fmt.Fprintln(os.Stdout, buildVersion())
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, serverRunConfig{dbPath: *dbPath, mtsPath: *mtsPath, listenAddr: *listenAddr, staticDir: *staticDir}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func buildVersion() string {
	return fmt.Sprintf("beat-server %s (%s, %s)", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
}

func configureLogger() {
	level := slog.LevelInfo
	switch os.Getenv("BEAT_LOG_LEVEL") {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}
