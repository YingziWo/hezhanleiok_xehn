package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// updateAsset must find the portable ZIP no matter what else a release carries.
func TestUpdateAssetPick(t *testing.T) {
	rel := &releaseInfo{Tag: "v1.2.3", Assets: []releaseAsset{
		{Name: "notes.txt", URL: "x"},
		{Name: "XEHN.1.2.3.zip", URL: "https://example/XEHN.1.2.3.zip"},
	}}
	a, ok := updateAsset(rel)
	if !ok || a.Name != "XEHN.1.2.3.zip" {
		t.Fatalf("picked %+v ok=%v", a, ok)
	}
	if rel.Version() != "1.2.3" {
		t.Fatalf("version = %q", rel.Version())
	}
	if _, ok := updateAsset(&releaseInfo{Tag: "v9"}); ok {
		t.Fatal("a release without a zip must not be installable")
	}
}

func makeZip(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// extractExe has to find the program wherever the packaging put it, and must
// reject an archive that carries something else.
func TestExtractExe(t *testing.T) {
	body := append([]byte("MZ"), make([]byte, 1024)...)
	got, err := extractExe(makeZip(t, "XEHN/xehn.exe", body))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("payload differs")
	}

	if _, err := extractExe(makeZip(t, "XEHN/readme.txt", []byte("hi"))); err == nil {
		t.Fatal("expected an error for an archive without xehn.exe")
	}
	if _, err := extractExe([]byte("not a zip")); err == nil {
		t.Fatal("expected an error for garbage")
	}
}

// applyUpdate is the rename dance: the running image is moved aside, the staged
// file takes its name, and the backup stays until the next start cleans it.
func TestApplyUpdate(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "xehn.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe+".new", []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := applyUpdate(exe); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatalf("exe content = %q", b)
	}
	if b, err := os.ReadFile(exe + ".old"); err != nil || string(b) != "old" {
		t.Fatalf("backup = %q err=%v", b, err)
	}
	cleanupUpdateLeftovers(exe)
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal("backup should be cleared on the next start")
	}
}

// A missing staged file must leave the working program exactly where it was.
func TestApplyUpdateRollback(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "xehn.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := applyUpdate(exe); err == nil {
		t.Fatal("expected a failure without a staged file")
	}
	if b, err := os.ReadFile(exe); err != nil || string(b) != "old" {
		t.Fatalf("program was lost: %q err=%v", b, err)
	}
}
