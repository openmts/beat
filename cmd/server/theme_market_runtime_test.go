package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestThemeMarketAllowPrivateToggle 覆盖主题市场私网放通开关的三条取值。
func TestThemeMarketAllowPrivateToggle(t *testing.T) {
	t.Setenv("BEAT_THEME_MARKET_ALLOW_PRIVATE", "")
	if allowed, err := themeMarketAllowPrivate(); err != nil || allowed {
		t.Fatalf("default private market = %v, %v", allowed, err)
	}
	t.Setenv("BEAT_THEME_MARKET_ALLOW_PRIVATE", "true")
	if allowed, err := themeMarketAllowPrivate(); err != nil || !allowed {
		t.Fatalf("enabled private market = %v, %v", allowed, err)
	}
	t.Setenv("BEAT_THEME_MARKET_ALLOW_PRIVATE", "yes-please")
	if _, err := themeMarketAllowPrivate(); err == nil {
		t.Fatal("non-boolean private market toggle was accepted")
	}
}

// TestNewThemeServicesFailures 覆盖主题服务目录不可用与开关解析失败两条启动失败。
func TestNewThemeServicesFailures(t *testing.T) {
	f := newCommercialFixture(t)
	t.Setenv("BEAT_THEME_MARKET_ALLOW_PRIVATE", "true")
	if _, _, err := newThemeServices(t.Context(), f.stores, t.TempDir()); err != nil {
		t.Fatalf("private market opt-in failed: %v", err)
	}
	blocked := t.TempDir()
	if err := os.WriteFile(filepath.Join(blocked, "themes"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := newThemeServices(t.Context(), f.stores, blocked); err == nil {
		t.Fatal("theme service accepted a file where its directory belongs")
	}
	t.Setenv("BEAT_THEME_MARKET_ALLOW_PRIVATE", "not-a-bool")
	if _, _, err := newThemeServices(t.Context(), f.stores, t.TempDir()); err == nil {
		t.Fatal("theme service accepted an invalid private market toggle")
	}
}

// TestOperationScannerConfiguration 覆盖运维扫描器配置的四种取值。
func TestOperationScannerConfiguration(t *testing.T) {
	t.Setenv("BEAT_OPERATIONS_SCANNER_EXECUTABLE", "")
	t.Setenv("BEAT_OPERATIONS_SCANNER_ARGS", "")
	scanner, err := operationScanner()
	if err != nil || scanner != nil {
		t.Fatalf("unconfigured scanner = %v, %v", scanner, err)
	}
	t.Setenv("BEAT_OPERATIONS_SCANNER_ARGS", `["--json"]`)
	if _, err := operationScanner(); err == nil {
		t.Fatal("scanner arguments without an executable were accepted")
	}
	executable := filepath.Join(t.TempDir(), "scanner")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEAT_OPERATIONS_SCANNER_EXECUTABLE", executable)
	t.Setenv("BEAT_OPERATIONS_SCANNER_ARGS", "not-json")
	if _, err := operationScanner(); err == nil {
		t.Fatal("non-JSON scanner arguments were accepted")
	}
	t.Setenv("BEAT_OPERATIONS_SCANNER_ARGS", `["--json"]`)
	scanner, err = operationScanner()
	if err != nil || scanner == nil {
		t.Fatalf("configured scanner = %v, %v", scanner, err)
	}
}
