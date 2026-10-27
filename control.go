package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Remote control: every install carries a code, and the maintainer publishes a
// list of codes that must stop working. It is the only lever that reaches a
// build already in someone's hands — no release, no rebuild, no local files.
//
// The check is deliberately fail-open: a client that cannot reach the list (a
// blocked GitHub, an offline laptop, an airplane) keeps working with the last
// verdict it did see. A control channel that bricks the app whenever the
// network is down would punish exactly the users it is meant to protect, and
// a reseller can always cut the network anyway — the point is to stop the
// ordinary case, not to win an arms race.
const (
	// controlListURL is the published stop list. Editing this file on the
	// default branch is what "remote stop" means; the client re-reads it at
	// startup and every controlCheckEvery while it runs.
	controlListURL    = "https://raw.githubusercontent.com/hezhanleiok/xehn/main/control.json"
	controlStateName  = "control-state.json"
	controlTimeout    = 8 * time.Second
	controlCheckEvery = 15 * time.Minute

	controlCodeLen  = 16
	controlCodeStep = 4
	// controlAlphabet drops 0/O/1/I/L: a code gets read out loud, typed from a
	// screenshot and pasted into an email, and every one of those confusions
	// turns into "the code does not match" support noise.
	controlAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
)

// controlList is the published document. StopAll is the blunt instrument for a
// leaked or resold build; Codes stops individual installs; Message is shown to
// the user instead of the default sentence when set.
type controlList struct {
	StopAll bool     `json:"stop_all"`
	Codes   []string `json:"stopped_codes"`
	Message string   `json:"message"`
}

// controlState is this install's verdict, cached on disk so a restart cannot
// launder a stopped client back into service before the network answers.
type controlState struct {
	Code      string    `json:"code"`
	Stopped   bool      `json:"stopped"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checked_at"`
	// Reached records whether the last attempt actually got an answer: an
	// unreachable list is not evidence either way, and the About page says so
	// instead of claiming "正常" for a check that never ran.
	Reached bool `json:"reached"`
}

// newControlCode mints a fresh code in XXXX-XXXX-XXXX-XXXX form.
func newControlCode() string {
	buf := make([]byte, controlCodeLen)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail on Windows; a time-seeded fallback still
		// produces a usable (if less unique) code rather than an empty one.
		now := time.Now().UnixNano()
		for i := range buf {
			buf[i] = byte(now >> (uint(i%8) * 8))
		}
	}
	out := make([]byte, 0, controlCodeLen+controlCodeLen/controlCodeStep)
	for i, b := range buf {
		if i > 0 && i%controlCodeStep == 0 {
			out = append(out, '-')
		}
		out = append(out, controlAlphabet[int(b)%len(controlAlphabet)])
	}
	return string(out)
}

// normalizeControlCode is the single spelling of a code: upper case, only the
// alphabet's characters, grouped by four. Comparison, storage and display all
// go through it, so "abcd efgh" and "ABCD-EFGH" cannot disagree.
func normalizeControlCode(s string) string {
	var clean []byte
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			clean = append(clean, byte(r))
		}
	}
	if len(clean) > controlCodeLen {
		clean = clean[:controlCodeLen]
	}
	out := make([]byte, 0, len(clean)+len(clean)/controlCodeStep)
	for i, b := range clean {
		if i > 0 && i%controlCodeStep == 0 {
			out = append(out, '-')
		}
		out = append(out, b)
	}
	return string(out)
}

// machineControlCode derives this machine's code from its stable identity.
// An empty result means the platform could not supply one (or the registry read
// failed), and the caller falls back to a random, persisted code.
//
// Deriving it — rather than drawing it once and storing it — is what makes the
// stop list hold: deleting settings.json would otherwise hand the same machine
// a brand-new code, and a stop that a `del` undoes is not a stop.
func machineControlCode() string {
	seed := strings.ToLower(strings.TrimSpace(machineSeed()))
	if seed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("xehn-control-v1|" + seed))
	out := make([]byte, 0, controlCodeLen+controlCodeLen/controlCodeStep)
	for i := 0; i < controlCodeLen; i++ {
		if i > 0 && i%controlCodeStep == 0 {
			out = append(out, '-')
		}
		out = append(out, controlAlphabet[int(sum[i])%len(controlAlphabet)])
	}
	return string(out)
}

// ensureControlCode settles this install's code and reports whether the
// settings file has to be rewritten.
func (s *clientSettings) ensureControlCode() bool {
	want := machineControlCode()
	if want == "" {
		// No machine identity to derive from: keep whatever the file carries,
		// or mint one. This is the only path where a code can be reset by
		// deleting files, which is why it is the fallback and not the default.
		want = normalizeControlCode(s.ControlCode)
		if want == "" {
			want = newControlCode()
		}
	}
	if want == s.ControlCode {
		return false
	}
	s.ControlCode = want
	return true
}

func controlStatePath() (string, error) {
	d, err := appDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, controlStateName), nil
}

func loadControlState() controlState {
	var st controlState
	p, err := controlStatePath()
	if err != nil {
		return st
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return st
	}
	if json.Unmarshal(raw, &st) != nil {
		return controlState{}
	}
	return st
}

func saveControlState(st controlState) error {
	p, err := controlStatePath()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, raw, 0o600)
}

// fetchControlList reads the published stop list. A non-200 answer is an error:
// a 404 means the file was moved, and treating that as "nobody is stopped"
// would silently disable the whole channel.
func fetchControlList(ctx context.Context) (*controlList, error) {
	return fetchControlListURL(ctx, controlListURL)
}

func fetchControlListURL(ctx context.Context, url string) (*controlList, error) {
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// The file is edited by hand on a branch; a cached copy would keep serving
	// the old verdict for hours after a stop.
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", appName+"/"+resolveVersion())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("control list: HTTP %d", resp.StatusCode)
	}
	var list controlList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("control list: %w", err)
	}
	return &list, nil
}

// evaluateControl fetches the list and turns it into this install's verdict.
// The returned state is always safe to store: when the fetch fails it carries
// the previous verdict forward and only clears Reached.
func evaluateControl(ctx context.Context, code string, prev controlState) controlState {
	list, err := fetchControlList(ctx)
	if err != nil {
		out := prev
		out.Code = normalizeControlCode(code)
		out.CheckedAt = time.Now()
		out.Reached = false
		out.Message = ""
		return out
	}
	return applyControlList(list, code, prev, time.Now())
}

// applyControlList is the pure half of the verdict: list in, state out, no
// network, no clock — which is what makes it testable.
func applyControlList(list *controlList, code string, prev controlState, now time.Time) controlState {
	out := prev
	out.Code = normalizeControlCode(code)
	out.CheckedAt = now
	out.Reached = true
	out.Stopped = list != nil && (list.StopAll || containsControlCode(list.Codes, out.Code))
	out.Message = ""
	if out.Stopped {
		out.Message = strings.TrimSpace(list.Message)
		if out.Message == "" {
			out.Message = defaultStopMessage
		}
	}
	return out
}

const defaultStopMessage = "该控制码已被远程停用，请联系作者说明使用场景。"

func containsControlCode(list []string, code string) bool {
	if code == "" {
		return false
	}
	for _, raw := range list {
		if normalizeControlCode(raw) == code {
			return true
		}
	}
	return false
}
