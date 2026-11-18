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

	"github.com/hourinX/Virelz/pkg/platform"
)

// shellConfig wires a fake platform into a session that reads from input.
func shellConfig(t *testing.T, input string, p platform.Platform) (Config, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	cfg := config(t, p)
	cfg.Stdin = strings.NewReader(input)
	cfg.Stdout = &out
	cfg.Stderr = &errOut
	return cfg, &out, &errOut
}

func demoVideo(url string) *fakePlatform {
	return &fakePlatform{
		name:   "demo",
		prefix: "https://demo.example/",
		video: &platform.Video{
			Site:    "demo",
			ID:      "v1",
			PageURL: "https://demo.example/v1",
			Title:   "Demo Clip",
			Media:   []platform.Media{{Kind: platform.MediaVideo, URL: url, Container: "mp4"}},
		},
	}
}

func TestShellDownloadsBareLink(t *testing.T) {
	url := serve(t, append(initSegment(), []byte("fragment")...))
	dir := t.TempDir()
	input := fmt.Sprintf("download %q -o %q\n", "https://demo.example/v1", dir)

	cfg, out, errOut := shellConfig(t, input, demoVideo(url))
	if code := runShell(context.Background(), withDefaults(cfg)); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Demo Clip.mp4") {
		t.Fatalf("expected the link to be downloaded: %q", out.String())
	}
	if !strings.Contains(out.String(), "SaveDir:") || !strings.Contains(out.String(), "Platform:") {
		t.Fatalf("the shell must print the aligned report: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "Demo Clip.mp4")); err != nil {
		t.Fatalf("file was not saved: %v, out = %q", err, out.String())
	}
	if !strings.Contains(out.String(), "Demo Clip") {
		t.Fatalf("output does not name the video: %q", out.String())
	}
}

func TestShellPrintsHelpAndExits(t *testing.T) {
	cfg, out, _ := shellConfig(t, "help\nexit\n", demoVideo("https://demo.example/v1"))
	if code := runShell(context.Background(), withDefaults(cfg)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	text := out.String()
	for _, want := range []string{"virelz> ", "resolve <link>", "platforms", "exit"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help is missing %q: %q", want, text)
		}
	}
}

func TestShellStopsOnEOF(t *testing.T) {
	cfg, _, _ := shellConfig(t, "help\n", demoVideo("https://demo.example/v1"))
	if code := runShell(context.Background(), withDefaults(cfg)); code != 0 {
		t.Fatalf("exit = %d after end of input", code)
	}
}

func TestShellRejectsUnknownCommand(t *testing.T) {
	cfg, out, _ := shellConfig(t, "wat\nexit\n", demoVideo("https://demo.example/v1"))
	if code := runShell(context.Background(), withDefaults(cfg)); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "unknown command") {
		t.Fatalf("expected an unknown command notice: %q", out.String())
	}
}

func TestShellReportsUnsupportedLink(t *testing.T) {
	cfg, _, errOut := shellConfig(t, "https://demo.example/nope\nexit\n", demoVideo("https://demo.example/v1"))
	runShell(context.Background(), withDefaults(cfg))
	if !strings.Contains(errOut.String(), "demo") {
		t.Fatalf("expected the platform error, got %q", errOut.String())
	}
}

func TestRunFallsBackWithoutTerminal(t *testing.T) {
	var out, errOut bytes.Buffer
	cfg := config(t, demoVideo("https://demo.example/v1"))
	cfg.Stdin = strings.NewReader("")
	cfg.Stdout = &out
	cfg.Stderr = &errOut
	cfg.Args = nil

	if code := Run(context.Background(), cfg); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("expected the help text when nothing is interactive: %q", out.String())
	}
	if strings.Contains(out.String(), "virelz> ") {
		t.Fatalf("shell should not start on a non-terminal stream: %q", out.String())
	}
}

func TestRunExecutesArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	cfg := config(t, demoVideo("https://demo.example/v1"))
	cfg.Stdout = &out
	cfg.Stderr = &errOut
	cfg.Args = []string{"platforms"}

	if code := Run(context.Background(), cfg); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "demo") {
		t.Fatalf("expected the platform list: %q", out.String())
	}
}

func TestStripProgramName(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"virelz", "-d", "link"}, []string{"-d", "link"}},
		{[]string{"virelz.exe", "-d", "link"}, []string{"-d", "link"}},
		{[]string{"VIRELZ", "platforms"}, []string{"platforms"}},
		{[]string{`.\virelz.exe`, "-h"}, []string{"-h"}},
		{[]string{"download", "link"}, []string{"download", "link"}},
		{[]string{"virelz"}, []string{}},
	}
	for _, c := range cases {
		got := stripProgramName(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("stripProgramName(%v) = %v, want %v", c.in, got, c.want)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Fatalf("stripProgramName(%v) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestShellAcceptsPastedCommand(t *testing.T) {
	url := serve(t, append(initSegment(), []byte("fragment")...))
	dir := t.TempDir()
	line := fmt.Sprintf("virelz -d %q -o %q", "https://demo.example/v1", dir)

	cfg, out, errOut := shellConfig(t, line+"\nexit\n", demoVideo(url))
	if code := runShell(context.Background(), withDefaults(cfg)); code != 0 {
		t.Fatalf("exit = %d, stderr = %q, out = %q", code, errOut.String(), out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "Demo Clip.mp4")); err != nil {
		t.Fatalf("pasted command did not download: %v, out = %q", err, out.String())
	}
}

func TestShellReportsTrailingSlashLink(t *testing.T) {
	cfg, out, errOut := shellConfig(t, `virelz -d "https://demo.example/v1/"`+"\nexit\n", demoVideo("https://demo.example/v1/"))
	runShell(context.Background(), withDefaults(cfg))
	if strings.Contains(out.String(), "unknown command") {
		t.Fatalf("pasted command was rejected: %q", out.String())
	}
	if !strings.Contains(out.String()+errOut.String(), "demo") {
		t.Fatalf("expected the platform to be invoked: out = %q, err = %q", out.String(), errOut.String())
	}
}

func TestLooksLikeLink(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://www.bilibili.com/video/BV1GJ411x7h7", "https://www.bilibili.com/video/BV1GJ411x7h7", true},
		{"www.bilibili.com/video/BV1GJ411x7h7", "https://www.bilibili.com/video/BV1GJ411x7h7", true},
		{"BV1GJ411x7h7", "https://www.bilibili.com/video/BV1GJ411x7h7", true},
		{"BV1GJ411x7h7 extra", "", false},
		{"platforms", "", false},
	}
	for _, c := range cases {
		got, ok := looksLikeLink(c.in)
		if ok != c.ok || got != c.want {
			t.Fatalf("looksLikeLink(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSplitCommandKeepsQuotedPaths(t *testing.T) {
	got := splitCommand(`download "https://demo.example/a b" -o 'C:\my videos'`)
	want := []string{"download", "https://demo.example/a b", "-o", `C:\my videos`}
	if len(got) != len(want) {
		t.Fatalf("split = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("split[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
