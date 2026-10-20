package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func seedThemePackage(t *testing.T, fixture *backupFixture) string {
	t.Helper()
	// 构造内容寻址主题包目录
	manifestBytes, _ := json.Marshal(map[string]any{
		"schema_version": 1, "id": "backup-theme", "name": "Backup Theme",
		"description": "d", "version": "1.0.0", "author": "a", "license": "MIT",
		"runtime": "declarative", "minimum_beat_version": "0.1.0", "preview": "preview.png",
	})
	preview := []byte("p")
	css := []byte("c")
	var buffer bytes.Buffer
	zipWriter := zip.NewWriter(&buffer)
	for name, content := range map[string][]byte{
		"beat-theme.json": manifestBytes, "preview.png": preview, "assets/theme.css": css,
	} {
		entry, _ := zipWriter.Create(name)
		_, _ = entry.Write(content)
	}
	_ = zipWriter.Close()
	archive := buffer.Bytes()
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))

	themesDir := filepath.Join(t.TempDir(), "themes")
	packageRoot := filepath.Join(themesDir, "packages", digest)
	if err := os.MkdirAll(packageRoot, 0o700); err != nil {
		t.Fatalf("mkdir theme package: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "archive.zip"), archive, 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	contentDir := filepath.Join(packageRoot, "content")
	if err := os.MkdirAll(contentDir, 0o700); err != nil {
		t.Fatalf("mkdir content: %v", err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "preview.png"), preview, 0o600); err != nil {
		t.Fatalf("write preview: %v", err)
	}
	assetsDir := filepath.Join(contentDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o700); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "theme.css"), css, 0o600); err != nil {
		t.Fatalf("write css: %v", err)
	}
	fixture.service.SetThemesDir(themesDir)

	// 写入主题包记录并设为活动主题
	now := time.Now().UTC()
	themeStore := store.NewThemeStore(fixture.sqlite.DB)
	pkg := &model.ThemePackage{
		ID: "pkg-1", ThemeID: "backup-theme", Name: "Backup Theme", Version: "1.0.0",
		Author: "a", Description: "d", License: "MIT", Runtime: model.ThemeRuntimeDeclarative,
		ManifestSchema: 1, MinimumBeatVersion: "0.1.0", ArchiveDigest: digest,
		ArchiveBytes: int64(len(archive)), ContentBytes: 2, ManifestJSON: string(manifestBytes),
		SourceType: model.ThemeSourceUpload, State: model.ThemeStateInstalled,
		ValidatedAt: now, CreatorID: "owner", CreatedAt: now, UpdatedAt: now,
	}
	if err := themeStore.CreatePackage(context.Background(), pkg); err != nil {
		t.Fatalf("create theme package: %v", err)
	}
	if err := themeStore.ActivatePackage(context.Background(), &model.ThemeActivation{
		ID: "act-1", PackageID: "pkg-1", ActorID: "owner",
		Reason: model.ThemeActivationReason, ActivatedAt: now,
	}, now); err != nil {
		t.Fatalf("activate theme: %v", err)
	}
	return digest
}

func TestBackupFormat2ThemeRoundTrip(t *testing.T) {
	fixture := newBackupFixture(t)
	digest := seedThemePackage(t, fixture)

	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	fixture.service.Wait()
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %v / %v", records, err)
	}
	archive, _, err := fixture.service.Open(t.Context(), record.ID)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	uploaded, err := fixture.service.ValidateUpload(t.Context(), archive)
	closeErr := archive.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("validate backup: %v / %v", err, closeErr)
	}
	if uploaded.State != "validated" {
		t.Fatalf("state = %s", uploaded.State)
	}

	// 恢复往返：主题应被恢复
	targetData := t.TempDir()
	targetBackups := filepath.Join(targetData, "backups")
	if err := os.Mkdir(targetBackups, 0o700); err != nil {
		t.Fatalf("mkdir target backups: %v", err)
	}
	sourcePath := filepath.Join(fixture.dataDir, "backups", uploaded.Filename)
	targetArchive := filepath.Join(targetBackups, uploaded.Filename)
	if err := copyFile(sourcePath, targetArchive, 0o600); err != nil {
		t.Fatalf("copy archive: %v", err)
	}
	journal := restoreJournal(uploaded.ID, targetArchive)
	if err := writeJournal(filepath.Join(targetData, "restore.pending.json"), journal); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	if err := ApplyPending(t.Context(), targetData,
		filepath.Join(targetData, "beat.db"), filepath.Join(targetData, "beat_mts")); err != nil {
		t.Fatalf("apply restore: %v", err)
	}
	restoredCSS, err := os.ReadFile(filepath.Join(targetData, "themes", "packages", digest, "content", "assets", "theme.css"))
	if err != nil || string(restoredCSS) != "c" {
		t.Fatalf("restored css = %q / %v", restoredCSS, err)
	}
	restoredArchive, err := os.ReadFile(filepath.Join(targetData, "themes", "packages", digest, "archive.zip"))
	if err != nil || len(restoredArchive) == 0 {
		t.Fatalf("restored archive missing: %v", err)
	}
}

func TestBackupFormat2RejectsTamperedTheme(t *testing.T) {
	fixture := newBackupFixture(t)
	seedThemePackage(t, fixture)

	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	fixture.service.Wait()
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 1 || records[0].State != "ready" {
		t.Fatalf("records = %v / %v", records, err)
	}
	ready := records[0]
	// 解包后删除一个主题文件条目再重打包，校验必须失败
	path := filepath.Join(fixture.dataDir, "backups", ready.Filename)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	tampered := dropZipEntry(t, content, "themes/")
	modified, err := os.CreateTemp(t.TempDir(), "tampered-*.zip")
	if err != nil {
		t.Fatalf("create tampered archive: %v", err)
	}
	if _, err := modified.Write(tampered); err != nil {
		t.Fatalf("write tampered archive: %v", err)
	}
	if err := modified.Close(); err != nil {
		t.Fatalf("close tampered archive: %v", err)
	}
	_ = os.WriteFile(path, tampered, 0o600)
	archive, _, openErr := fixture.service.Open(t.Context(), ready.ID)
	if openErr != nil {
		t.Fatalf("open backup: %v", openErr)
	}
	tamperedResult, err := fixture.service.ValidateUpload(t.Context(), archive)
	_ = archive.Close()
	if err == nil {
		t.Fatalf("tampered archive validated: %#v", tamperedResult)
	}
	_ = record
}

// dropZipEntry 重打包归档并删除第一个匹配前缀的条目。
func dropZipEntry(t *testing.T, content []byte, prefix string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	dropped := false
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, prefix) && !dropped {
			dropped = true
			continue
		}
		entry, createErr := writer.CreateHeader(&zip.FileHeader{Name: file.Name, Method: zip.Deflate})
		if createErr != nil {
			t.Fatalf("create entry: %v", createErr)
		}
		rc, openErr := file.Open()
		if openErr != nil {
			t.Fatalf("open entry: %v", openErr)
		}
		_, copyErr := io.Copy(entry, rc)
		_ = rc.Close()
		if copyErr != nil {
			t.Fatalf("copy entry: %v", copyErr)
		}
	}
	if !dropped {
		t.Fatalf("no theme entry dropped")
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func TestBackupFormat1StillValid(t *testing.T) {
	// 无主题时备份仍为格式 1，且可校验
	fixture := newBackupFixture(t)
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	fixture.service.Wait()
	archive, _, err := fixture.service.Open(t.Context(), record.ID)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	uploaded, err := fixture.service.ValidateUpload(t.Context(), archive)
	closeErr := archive.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("validate format1: %v / %v", err, closeErr)
	}
	if uploaded.State != "validated" {
		t.Fatalf("state = %s", uploaded.State)
	}
}

func TestBackupCopyThemesInvalidDirectory(t *testing.T) {
	fixture := newBackupFixture(t)
	// themesDir 指向一个文件而非目录 → copyThemes 报错
	badThemes := filepath.Join(t.TempDir(), "themes")
	if err := os.WriteFile(badThemes, []byte("x"), 0o600); err != nil {
		t.Fatalf("write themes file: %v", err)
	}
	fixture.service.SetThemesDir(badThemes)
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	fixture.service.Wait()
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 1 || records[0].State != "failed" {
		t.Fatalf("records = %v / %v", records, err)
	}
	_ = record
}

func TestValidatePayloadsFormat2Rejections(t *testing.T) {
	// 构造一个格式 2 manifest，但其 themes 条目与磁盘不一致
	root := t.TempDir()
	manifest := model.BackupManifest{
		FormatVersion: 2, CreatedAt: time.Now().UTC(), SnapshotCutoff: time.Now().UTC(),
		PayloadSizes: map[string]int64{},
		Checksums:    map[string]string{},
		ThemeFiles: map[string]model.BackupThemeFile{
			"themes/digest/archive.zip": {Path: "themes/digest/archive.zip", Size: 1, SHA256: "x"},
		},
	}
	// 缺 sqlite/metrics 也会报错；这里先验证缺失主题归档被拒绝
	// 写入一个虚假的 sqlite/metrics 以满足基本校验
	_ = os.WriteFile(filepath.Join(root, sqliteEntry), []byte("sqlite"), 0o600)
	_ = os.WriteFile(filepath.Join(root, metricsEntry), []byte("metrics"), 0o600)
	manifest.PayloadSizes[sqliteEntry] = 6
	manifest.Checksums[sqliteEntry] = "0"
	manifest.PayloadSizes[metricsEntry] = 7
	manifest.Checksums[metricsEntry] = "0"
	if err := validatePayloads(root, manifest); err == nil {
		t.Fatal("format 2 manifest with missing theme archive accepted")
	}

	// 活动主题存在但主题资产缺失
	manifest.PayloadSizes["themes/digest/archive.zip"] = 1
	manifest.Checksums["themes/digest/archive.zip"] = "x"
	_ = os.WriteFile(filepath.Join(root, "themes", "digest", "archive.zip"), []byte("a"), 0o600)
	if err := validatePayloads(root, manifest); err == nil {
		t.Fatal("format 2 with mismatched theme metadata accepted")
	}
}
