// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hourinX/Virelz/internal/downloader"
)

// TestShellDownloadsRealSegment drives the session the way a person would,
// typing a link and an output folder, against a local HTTP server.
func TestShellDownloadsRealSegment(t *testing.T) {
	if os.Getenv("VIRELZ_E2E") == "" {
		t.Skip("set VIRELZ_E2E=1 to run the interactive session end to end")
	}

	body := append(initSegment(), []byte("payload")...)
	url := serve(t, body)
	dir := t.TempDir()

	p := demoVideo(url)
	p.video.Media[0].Headers = nil

	input := strings.Join([]string{
		fmt.Sprintf("download %q -o %q", p.video.PageURL, dir),
		"platforms",
		"exit",
	}, "\n") + "\n"

	var out, errOut bytes.Buffer
	cfg := config(t, p)
	cfg.Stdin = strings.NewReader(input)
	cfg.Stdout = &out
	cfg.Stderr = &errOut
	cfg.Download = downloader.Options{}

	if code := runShell(context.Background(), withDefaults(cfg)); code != 0 {
		t.Fatalf("exit = %d, stderr = %q, out = %q", code, errOut.String(), out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "Demo Clip.mp4")); err != nil {
		t.Fatalf("download missing: %v, out = %q", err, out.String())
	}
	if !strings.Contains(out.String(), "demo") {
		t.Fatalf("platforms command printed nothing: %q", out.String())
	}
}
