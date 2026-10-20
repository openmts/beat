package main

import (
	"context"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/beat/backend/internal/buildinfo"
)

func TestRunStopsWithContext(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	errorsCh := make(chan error, 1)
	address := availableServerAddress(t)
	dbPath := filepath.Join(t.TempDir(), "beat.db")
	mtsPath := filepath.Join(t.TempDir(), "mts")
	staticDir := t.TempDir()
	go func() {
		errorsCh <- run(ctx, serverRunConfig{dbPath: dbPath, mtsPath: mtsPath, listenAddr: address, staticDir: staticDir})
	}()
	waitForServerHealth(t, address, errorsCh)
	cancel()
	if err := <-errorsCh; err != nil {
		t.Fatalf("run: %v", err)
	}
}

func availableServerAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate server address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release server address: %v", err)
	}
	return address
}

func waitForServerHealth(t *testing.T, address string, errorsCh <-chan error) {
	t.Helper()
	// race 检出的服务端启动要跑迁移和后台工作线程，加载较重时需要更宽的健康等待窗口。
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errorsCh:
			t.Fatalf("server exited before becoming healthy: %v", err)
		default:
		}
		response, err := http.Get("http://" + address + "/healthz")
		if err == nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				t.Fatalf("close health response: %v", closeErr)
			}
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not become healthy")
}

func TestRunRejectsInvalidDatabasePath(t *testing.T) {
	t.Chdir(t.TempDir())
	err := run(context.Background(), serverRunConfig{dbPath: "/dev/null/beat.db", mtsPath: t.TempDir(), listenAddr: "127.0.0.1:0", staticDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "create data directory") {
		t.Fatalf("error = %v, want data directory error", err)
	}
}

func TestRunRejectsInvalidTrustedProxyConfiguration(t *testing.T) {
	t.Setenv("BEAT_TRUSTED_PROXIES", "invalid")
	err := run(context.Background(), serverRunConfig{dbPath: filepath.Join(t.TempDir(), "beat.db"), mtsPath: filepath.Join(t.TempDir(), "mts"), listenAddr: "127.0.0.1:0", staticDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "configure trusted proxies") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunRejectsInvalidListenAddress(t *testing.T) {
	t.Chdir(t.TempDir())
	err := run(
		context.Background(), serverRunConfig{dbPath: filepath.Join(t.TempDir(), "beat.db"), mtsPath: filepath.Join(t.TempDir(), "mts"), listenAddr: "invalid-address", staticDir: t.TempDir()},
	)
	if err == nil || !strings.Contains(err.Error(), "http server") {
		t.Fatalf("error = %v, want HTTP server error", err)
	}
}

func TestRunRejectsInvalidMTSPath(t *testing.T) {
	t.Chdir(t.TempDir())
	err := run(
		context.Background(), serverRunConfig{dbPath: filepath.Join(t.TempDir(), "beat.db"), mtsPath: "/dev/null/mts", listenAddr: "127.0.0.1:0", staticDir: t.TempDir()},
	)
	if err == nil || !strings.Contains(err.Error(), "initialize mts store") {
		t.Fatalf("error = %v, want MTS initialization error", err)
	}
}

func TestRunInitializationFailures(t *testing.T) {
	t.Chdir(t.TempDir())
	tests := []struct {
		name      string
		prepare   func(t *testing.T, dataDir string) string
		wantError string
	}{
		{name: "pending restore", prepare: func(t *testing.T, dataDir string) string {
			writeServerTestFile(t, filepath.Join(dataDir, "restore.pending.json"), "{")
			return filepath.Join(dataDir, "beat.db")
		}, wantError: "apply pending restore"},
		{name: "database directory", prepare: func(t *testing.T, dataDir string) string {
			dbPath := filepath.Join(dataDir, "beat.db")
			if err := os.Mkdir(dbPath, 0o700); err != nil {
				t.Fatalf("create database directory: %v", err)
			}
			return dbPath
		}, wantError: "initialize sqlite store"},
		{name: "administrator key", prepare: func(t *testing.T, dataDir string) string {
			writeServerTestFile(t, filepath.Join(dataDir, "admin-data.key"), "short")
			return filepath.Join(dataDir, "beat.db")
		}, wantError: "administrator data encryption"},
		{name: "backup directory", prepare: func(t *testing.T, dataDir string) string {
			writeServerTestFile(t, filepath.Join(dataDir, "backups"), "file")
			return filepath.Join(dataDir, "beat.db")
		}, wantError: "initialize backup service"},
		{name: "ssh directory", prepare: func(t *testing.T, dataDir string) string {
			writeServerTestFile(t, filepath.Join(dataDir, "ssh"), "file")
			return filepath.Join(dataDir, "beat.db")
		}, wantError: "initialize ssh client"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			dbPath := test.prepare(t, dataDir)
			err := run(context.Background(), serverRunConfig{dbPath: dbPath, mtsPath: filepath.Join(dataDir, "mts"), listenAddr: "127.0.0.1:0", staticDir: t.TempDir()})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func writeServerTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestMainStartsAndStopsOnSignal(t *testing.T) {
	t.Chdir(t.TempDir())
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	})
	dataDir := t.TempDir()
	address := availableServerAddress(t)
	flag.CommandLine = flag.NewFlagSet("beat-server", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = []string{"beat-server",
		"-db-path", filepath.Join(dataDir, "beat.db"),
		"-mts-path", filepath.Join(dataDir, "mts"),
		"-listen-addr", address,
		"-static-dir", t.TempDir(),
	}
	healthy := make(chan bool, 1)
	go func() {
		// 健康检查失败也必须发出信号：否则 main() 会一直运行到整包超时，把失败变成挂死。
		healthy <- waitForHTTPHealth(address)
		process, err := os.FindProcess(os.Getpid())
		if err != nil {
			t.Errorf("find self process: %v", err)
			return
		}
		if err := process.Signal(syscall.SIGTERM); err != nil {
			t.Errorf("signal self: %v", err)
		}
	}()
	main()
	if !<-healthy {
		t.Error("server did not become healthy before shutdown")
	}
}

func waitForHTTPHealth(address string) bool {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get("http://" + address + "/healthz")
		if err == nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				return false
			}
			if response.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestBuildVersionIncludesBuildInfo(t *testing.T) {
	version := buildVersion()
	for _, part := range []string{"beat-server", buildinfo.Version, buildinfo.Commit, buildinfo.Date} {
		if !strings.Contains(version, part) {
			t.Fatalf("version %q does not contain %q", version, part)
		}
	}
}

func TestConfigureLoggerLevels(t *testing.T) {
	for _, level := range []string{"debug", "warn", "error", "unknown"} {
		t.Setenv("BEAT_LOG_LEVEL", level)
		configureLogger()
	}
	t.Setenv("BEAT_LOG_LEVEL", "")
	configureLogger()
}

func TestMainVersionFlagPrintsAndExits(t *testing.T) {
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	})
	flag.CommandLine = flag.NewFlagSet("beat-server", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = []string{"beat-server", "-version"}
	stdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = stdout }()
	main()
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close pipe writer: %v", closeErr)
	}
	content, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read version output: %v", readErr)
	}
	if !strings.Contains(string(content), "beat-server") {
		t.Fatalf("version output = %q", string(content))
	}
}
