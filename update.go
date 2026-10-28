package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Self-update: check, download the release ZIP, pull xehn.exe out of it, and
// swap it in. The swap is a rename dance — Windows refuses to overwrite a
// running image but happily renames one — so the new binary can be put in place
// while this process is still alive, and the restart happens after it exits.
const (
	ghReleaseURL   = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases/latest"
	updateTimeout  = 10 * time.Minute
	updateMinBytes = 8 << 20 // a real xehn.exe is ~20 MB; anything smaller is an error page
)

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type releaseInfo struct {
	Tag    string         `json:"tag_name"`
	Assets []releaseAsset `json:"assets"`
}

// Version is the tag without its leading v, which is what the version
// comparison speaks.
func (r *releaseInfo) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// fetchLatestRelease reads the release the updater would install. Unlike
// fetchLatestVersion this needs the asset list, so it asks the API directly
// rather than the cached version file.
func fetchLatestRelease(ctx context.Context) (*releaseInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ghReleaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", appName+"/"+resolveVersion())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("发布页返回 HTTP %d", resp.StatusCode)
	}
	var rel releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.Tag == "" {
		return nil, fmt.Errorf("发布页没有返回版本号")
	}
	return &rel, nil
}

// updateAsset picks the portable ZIP out of a release.
func updateAsset(rel *releaseInfo) (releaseAsset, bool) {
	for _, a := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(a.Name), ".zip") {
			return a, true
		}
	}
	return releaseAsset{}, false
}

// downloadUpdate fetches the release archive and leaves the new program next to
// the running one as <exe>.new. Nothing is replaced yet: the caller swaps the
// files once the transfer is proven good.
func downloadUpdate(ctx context.Context, rel *releaseInfo, logf func(string)) (string, error) {
	if logf == nil {
		logf = func(string) {}
	}
	asset, ok := updateAsset(rel)
	if !ok {
		return "", fmt.Errorf("该版本没有可下载的 zip")
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	staged := exe + ".new"

	logf(fmt.Sprintf("下载 %s（%.1f MB）…", asset.Name, float64(asset.Size)/(1<<20)))
	data, err := fetchUpdateZip(ctx, asset.URL)
	if err != nil {
		return "", err
	}
	logf(fmt.Sprintf("下载完成（%.1f MB），校验中…", float64(len(data))/(1<<20)))

	bin, err := extractExe(data)
	if err != nil {
		return "", err
	}
	if len(bin) < updateMinBytes {
		return "", fmt.Errorf("更新包里的程序大小异常（%d 字节）", len(bin))
	}
	// A PE image starts with "MZ"; anything else means we grabbed an error page
	// or the archive layout changed, and installing it would brick the client.
	if bin[0] != 'M' || bin[1] != 'Z' {
		return "", fmt.Errorf("更新包里的程序不是有效的 exe")
	}
	if err := os.WriteFile(staged, bin, 0o755); err != nil {
		return "", err
	}
	logf(fmt.Sprintf("已就绪：%s", filepath.Base(staged)))
	return staged, nil
}

func fetchUpdateZip(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", appName+"/"+resolveVersion())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512<<20))
}

// extractExe pulls xehn.exe out of the release archive, wherever the packaging
// put it (today: XEHN/xehn.exe).
func extractExe(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("更新包无法解析：%w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Base(f.Name), "xehn.exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 128<<20))
	}
	return nil, fmt.Errorf("更新包里没有 xehn.exe")
}

// applyUpdate swaps the staged program in. The running image is renamed first
// (allowed), then the staged file takes its place; a failure rolls the rename
// back so the install never ends up with no program at all.
func applyUpdate(exe string) error {
	backup := exe + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(exe, backup); err != nil {
		return fmt.Errorf("无法替换程序（可能被其它实例占用）：%w", err)
	}
	if err := os.Rename(exe+".new", exe); err != nil {
		_ = os.Rename(backup, exe)
		return fmt.Errorf("无法写入新程序：%w", err)
	}
	return nil
}

// cleanupUpdateLeftovers runs at startup: a staged file means an earlier update
// never completed, and a backup means the swap succeeded — the new binary is
// running, so the old one can go.
func cleanupUpdateLeftovers(exe string) {
	_ = os.Remove(exe + ".new")
	_ = os.Remove(exe + ".old")
}
