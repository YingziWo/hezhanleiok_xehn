//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/lxn/walk"

	"github.com/xhwarp/warp-client/internal/core"
)

// ---------------------------------------------------------------------------
// Modal dialogs
// ---------------------------------------------------------------------------

// promptText shows a small modal input box and returns the entered text. It runs
// on the GUI thread (actions always do), which is what walk's modal loop needs.
func (u *natUI) promptText(title, hint, value string) (string, bool) {
	dlg, err := walk.NewDialogWithFixedSize(u.mw)
	if err != nil {
		u.addLog(tr("打开输入框失败")+": "+err.Error(), true)
		return "", false
	}
	defer dlg.Dispose()
	dlg.SetTitle(title)

	vbox := walk.NewVBoxLayout()
	_ = vbox.SetMargins(walk.Margins{HNear: 16, VNear: 14, HFar: 16, VFar: 14})
	_ = vbox.SetSpacing(8)
	if err := dlg.SetLayout(vbox); err != nil {
		u.addLog(tr("输入框布局失败")+": "+err.Error(), true)
		return "", false
	}

	if hint != "" {
		if lbl, lerr := walk.NewLabel(dlg); lerr == nil {
			_ = lbl.SetText(hint)
		}
	}
	le, err := walk.NewLineEdit(dlg)
	if err != nil {
		u.addLog(tr("输入框创建失败")+": "+err.Error(), true)
		return "", false
	}
	_ = le.SetText(value)
	le.SetMaxLength(512)

	row, err := walk.NewComposite(dlg)
	if err != nil {
		u.addLog(tr("输入框创建失败")+": "+err.Error(), true)
		return "", false
	}
	hbox := walk.NewHBoxLayout()
	_ = hbox.SetMargins(walk.Margins{})
	_ = hbox.SetSpacing(8)
	_ = row.SetLayout(hbox)

	ok, err := walk.NewPushButton(row)
	if err != nil {
		u.addLog(tr("输入框创建失败")+": "+err.Error(), true)
		return "", false
	}
	_ = ok.SetText(tr("确定"))
	cancel, err := walk.NewPushButton(row)
	if err != nil {
		u.addLog(tr("输入框创建失败")+": "+err.Error(), true)
		return "", false
	}
	_ = cancel.SetText(tr("取消"))
	_ = dlg.SetDefaultButton(ok)
	_ = dlg.SetCancelButton(cancel)
	ok.Clicked().Attach(func() { dlg.Accept() })
	cancel.Clicked().Attach(func() { dlg.Cancel() })

	if dlg.Run() != walk.DlgCmdOK {
		return "", false
	}
	return le.Text(), true
}

// confirm shows a yes/no dialog. The OK button carries the affirmative answer so
// Enter accepts and Esc cancels, which is what the keyboard expects.
func (u *natUI) confirm(title, question string) bool {
	dlg, err := walk.NewDialogWithFixedSize(u.mw)
	if err != nil {
		u.addLog(tr("打开对话框失败")+": "+err.Error(), true)
		return false
	}
	defer dlg.Dispose()
	dlg.SetTitle(title)

	vbox := walk.NewVBoxLayout()
	_ = vbox.SetMargins(walk.Margins{HNear: 16, VNear: 14, HFar: 16, VFar: 14})
	_ = vbox.SetSpacing(10)
	if err := dlg.SetLayout(vbox); err != nil {
		return false
	}
	if lbl, lerr := walk.NewLabel(dlg); lerr == nil {
		_ = lbl.SetText(question)
	}
	row, err := walk.NewComposite(dlg)
	if err != nil {
		return false
	}
	hbox := walk.NewHBoxLayout()
	_ = hbox.SetMargins(walk.Margins{})
	_ = hbox.SetSpacing(8)
	_ = row.SetLayout(hbox)

	ok, err := walk.NewPushButton(row)
	if err != nil {
		return false
	}
	_ = ok.SetText(tr("确定"))
	cancel, err := walk.NewPushButton(row)
	if err != nil {
		return false
	}
	_ = cancel.SetText(tr("取消"))
	_ = dlg.SetDefaultButton(ok)
	_ = dlg.SetCancelButton(cancel)
	ok.Clicked().Attach(func() { dlg.Accept() })
	cancel.Clicked().Attach(func() { dlg.Cancel() })

	return dlg.Run() == walk.DlgCmdOK
}

// ---------------------------------------------------------------------------
// Shell helpers
// ---------------------------------------------------------------------------

// openURL hands a URL to the shell's default handler.
func openURL(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// openDir reveals a path in Explorer; a file is revealed by selecting it.
func openDir(path string) {
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		_ = exec.Command("explorer", "/select,"+path).Start()
		return
	}
	_ = exec.Command("explorer", path).Start()
}

func (u *natUI) openDataDir() {
	dir, err := appDir()
	if err != nil {
		u.addLog(err.Error(), true)
		return
	}
	openDir(dir)
}

// noticeFilePath locates the third-party notices shipped next to the binary.
func noticeFilePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	p := filepath.Join(filepath.Dir(exe), "THIRD_PARTY_NOTICES.md")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("not found: %s", p)
	}
	return p, nil
}

func opensNotice() error {
	p, err := noticeFilePath()
	if err != nil {
		return err
	}
	openDir(p)
	return nil
}

// mustCoreDir is the core directory, falling back to the process working
// directory when the app home cannot be resolved.
func mustCoreDir() string {
	if d, err := coreDir(); err == nil {
		return d
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// diagText is what the diagnostics button copies: everything needed to describe
// this install without asking the user to hunt through folders.
func (u *natUI) diagText() string {
	dir, _ := appDir()
	sp, _ := settingsPath()
	cDir, _ := coreDir()
	var b strings.Builder
	b.WriteString(appName + " " + shortVersion() + "\r\n")
	b.WriteString("build: " + guiBuildKind() + "\r\n")
	b.WriteString("lang/theme: " + u.m.set.Lang + " / " + u.m.set.Theme + "\r\n")
	b.WriteString("data: " + dir + "\r\n")
	b.WriteString("core: " + core.CoreInDir(cDir) + "\r\n")
	b.WriteString("core version: " + u.m.coreVersion + "\r\n")
	b.WriteString("settings: " + sp + "\r\n")
	if st, err := loadState(); err == nil {
		b.WriteString("state: " + st.statusLine() + "\r\n")
	}
	for _, l := range u.m.logs {
		b.WriteString("log " + l.at.Format("15:04:05") + " " + l.text + "\r\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Autostart (HKCU\...\Run) — the registry is the source of truth
// ---------------------------------------------------------------------------

const (
	autoRunKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	autoRunValue = "WARPClient"
)

func setAutoStart(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, autoRunKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(autoRunValue); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Quoted: the path may contain spaces, and Run parses it as a command line.
	return k.SetStringValue(autoRunValue, strconv.Quote(exe))
}

func autoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, autoRunKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	if _, _, err := k.GetStringValue(autoRunValue); err != nil {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Preference plumbing shared by every page
// ---------------------------------------------------------------------------

// persistSession copies the session choices into the preference file and saves
// it, so a protocol or switch the user picked survives the process. Called when
// a connect starts and when the window closes.
func (u *natUI) persistSession() {
	m := u.m
	m.set.Proto = m.proto
	m.set.Tun = m.tun
	m.set.Rescan = m.rescan
	m.set.Psiphon = m.psiphon
	m.set.OuterProto = m.outerProto
	m.set.InnerProto = m.innerProto
	m.set.Page = m.page
	if err := saveSettings(m.set); err != nil {
		u.addLog(tr("设置保存失败: ")+err.Error(), true)
	}
}

// mutateSettings applies fn to the persisted preferences, saves them and marks
// the session as needing a reconnect (a live session read its settings when it
// started; changing them now cannot reach it).
func (u *natUI) mutateSettings(fn func(*clientSettings)) {
	fn(&u.m.set)
	if err := saveSettings(u.m.set); err != nil {
		u.addLog(tr("设置保存失败: ")+err.Error(), true)
	} else if u.m.connected {
		u.m.setDirty = true
	}
	u.invalidate()
}

// ---------------------------------------------------------------------------
// Small display helpers
// ---------------------------------------------------------------------------

// currentFamily maps the engine's boolean onto the address-family choice the
// user sees: the flag has always meant "use the IPv6 endpoint pools instead of
// the IPv4 ones", so it is a choice, not a switch that can be on independently.
func currentFamily(v6 bool) string {
	if v6 {
		return "v6"
	}
	return "v4"
}

func guiBuildKind() string {
	if windowsGUIBuild == "1" {
		return "Windows GUI (windowsgui)"
	}
	return "Windows console"
}

func coreSizeText(n int64) string {
	if n <= 0 {
		return tr("未知")
	}
	return sizeText(n)
}

func orEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// shortVersion is what the header and the about card call this build. A local,
// untagged build reports "dev", which would otherwise render as "vdev".
func shortVersion() string {
	if v := resolveVersion(); v != "dev" {
		return "v" + v
	}
	return tr("开发版")
}

// ---------------------------------------------------------------------------
// DNS presets
// ---------------------------------------------------------------------------

type dnsPreset struct {
	label string
	value string
}

var dnsPresets = []dnsPreset{
	{"阿里", "223.5.5.5, 223.6.6.6"},
	{"腾讯", "119.29.29.29, 182.254.116.116"},
	{"Cloudflare", "1.1.1.1, 1.0.0.1"},
	{"Google", "8.8.8.8, 8.8.4.4"},
}

func dnsPresetIDs() []string {
	out := make([]string, 0, len(dnsPresets))
	for i := range dnsPresets {
		out = append(out, strconv.Itoa(i))
	}
	return out
}

func dnsPresetLabels() []string {
	out := make([]string, 0, len(dnsPresets))
	for i := range dnsPresets {
		out = append(out, dnsPresets[i].label)
	}
	return out
}

func currentDNSPreset(v string) string {
	norm := normaliseDNS(v)
	if norm == "" {
		return ""
	}
	for i := range dnsPresets {
		if normaliseDNS(dnsPresets[i].value) == norm {
			return strconv.Itoa(i)
		}
	}
	return ""
}

func normaliseDNS(v string) string {
	fields := fieldList(v)
	for i := range fields {
		fields[i] = strings.ToLower(fields[i])
	}
	return strings.Join(fields, ",")
}
