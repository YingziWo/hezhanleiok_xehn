package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var versionRe = regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)

// BinaryVersion runs `mihomo -v` and returns the bare semantic version
// ("1.19.32"). It works for both official releases ("Mihomo Meta v1.19.32 …")
// and older/forked builds that print no "v" prefix.
func BinaryVersion(bin string) (string, error) {
	cmd := exec.Command(bin, "-v")
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("core: %s -v: %w", filepath.Base(bin), err)
	}
	if m := versionRe.FindSubmatch(out); m != nil {
		return string(m[1]), nil
	}
	return "", fmt.Errorf("core: cannot parse version from %q", strings.TrimSpace(string(out)))
}

// TestConfig asks the core to validate configYAML via `mihomo -t` and returns
// an error carrying the core's own complaint when it rejects the file. This is
// the ground truth for capability probing: instead of guessing from a version
// number whether an outbound type exists, the actual binary parses a minimal
// per-protocol config and answers.
func TestConfig(bin, workDir string, configYAML []byte) error {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	probe := filepath.Join(workDir, "capability-probe.yaml")
	if err := os.WriteFile(probe, configYAML, 0o600); err != nil {
		return err
	}
	defer os.Remove(probe)

	cmd := exec.Command(bin, "-t", "-d", workDir, "-f", probe)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
