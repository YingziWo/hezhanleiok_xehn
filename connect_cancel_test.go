package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Cancelling a connect must stop the pipeline, not merely change the button.
// Before the checkpoints were added the pipeline ran to completion and reported
// success minutes after the user pressed cancel — for plain WARP as well as for
// the OpenVPN exit.
func TestConnectStopsOnCancelledContext(t *testing.T) {
	t.Setenv("WARP_CLIENT_HOME", t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := runConnectCmd(ctx, options{mixedPort: 7899, apiPort: 9099, proto: protoAWG})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("connect with a cancelled context must not succeed")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled so the GUI can tell cancel from failure", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("cancelled connect took %v; it must abort at the first checkpoint", elapsed)
	}
}
