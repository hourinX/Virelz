// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hourinX/Virelz/assets"
)

const promptText = "virelz> " // shown while waiting for the next command

// Run starts the interactive shell when no arguments were given on a terminal,
// and falls back to one-shot execution otherwise.
func Run(ctx context.Context, cfg Config) int {
	cfg = withDefaults(cfg)

	if len(cfg.Args) > 0 {
		return Execute(ctx, cfg, cfg.Args)
	}

	if !isTerminalReader(cfg.Stdin) {
		if !ensureConsole() {
			return Execute(ctx, cfg, nil)
		}
		cfg.Stdin = os.Stdin
		cfg.Stdout = os.Stdout
		cfg.Stderr = os.Stderr
	}
	return runShell(ctx, cfg)
}

// runShell executes commands from stdin until the user leaves.
func runShell(ctx context.Context, cfg Config) int {
	in := bufio.NewReader(cfg.Stdin)
	out := cfg.Stdout
	state := &shellState{}

	printBanner(out, cfg)

	for {
		fmt.Fprint(out, promptText)
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(out)
				return state.code
			}
			fmt.Fprintf(out, "%s: %v\n", cfg.Name, err)
			return 1
		}

		line = strings.TrimSpace(line)
		if line == "" {
			if errors.Is(err, io.EOF) {
				return state.code
			}
			continue
		}

		code, quit := state.execute(ctx, cfg, line)
		state.code = code
		if quit {
			return state.code
		}
		if errors.Is(err, io.EOF) {
			return state.code
		}
	}
}

// printBanner shows the version and the two commands a new user needs.
func printBanner(out io.Writer, cfg Config) {
	fmt.Fprintf(out, "%s %s - resolve and download videos\n", cfg.Name, cfg.Version)
	fmt.Fprintf(out, "Type a link to download it, %q for all commands, %q to leave.\n\n", "help", "exit")
}

// shellState keeps the exit code of the last command between prompts.
type shellState struct {
	code int // exit code reported when the session ends
}

// stripProgramName drops a leading virelz or virelz.exe so a pasted one-shot
// command works unchanged inside the prompt.
func stripProgramName(args []string) []string {
	if len(args) == 0 {
		return args
	}
	switch strings.ToLower(args[0]) {
	case "virelz", "virelz.exe", ".\\virelz.exe", "./virelz", "./virelz.exe":
		return args[1:]
	}
	return args
}

// execute runs one shell line and reports whether the session should end.
func (s *shellState) execute(ctx context.Context, cfg Config, line string) (int, bool) {
	line = strings.TrimSpace(line)
	args := stripProgramName(splitCommand(line))

	if len(args) == 0 {
		printShellHelp(cfg.Stdout, cfg)
		return 0, false
	}

	switch strings.ToLower(args[0]) {
	case "exit", "quit", "q", ":q":
		return s.code, true
	case "help", "?":
		printShellHelp(cfg.Stdout, cfg)
		return 0, false
	case "clear", "cls":
		fmt.Fprint(cfg.Stdout, "\x1b[2J\x1b[H")
		return s.code, false
	}

	if len(args) == 1 {
		if link, ok := looksLikeLink(args[0]); ok {
			return Execute(ctx, cfg, []string{"download", link}), false
		}
	}

	if !knownCommand(args[0]) && !strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(cfg.Stdout, "unknown command %q, type %q for the list\n", args[0], "help")
		return 1, false
	}

	return Execute(ctx, cfg, args), false
}

// splitCommand splits a shell line on whitespace, honouring single and double
// quotes so a path with spaces survives.
func splitCommand(line string) []string {
	var (
		args  []string
		field strings.Builder
		quote rune
		has   bool
	)

	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			field.WriteRune(r)
			has = true
		case r == '\'' || r == '"':
			quote = r
			has = true
		case r == ' ' || r == '\t':
			if has {
				args = append(args, field.String())
				field.Reset()
				has = false
			}
		default:
			field.WriteRune(r)
			has = true
		}
	}

	if has {
		args = append(args, field.String())
	}
	if len(args) == 0 {
		return []string{""}
	}
	return args
}

// looksLikeLink accepts a bare line that is clearly meant as a video link.
func looksLikeLink(line string) (string, bool) {
	if strings.ContainsAny(line, " \t\"'") {
		return "", false
	}
	switch {
	case strings.Contains(line, "://"):
		return line, true
	case strings.HasPrefix(line, "www."):
		return "https://" + line, true
	case strings.HasPrefix(line, "BV") && len(line) == 12:
		return "https://www.bilibili.com/video/" + line, true
	}
	return "", false
}

// knownCommand reports whether the first word names a real subcommand.
func knownCommand(name string) bool {
	switch name {
	case "download", "resolve", "platforms", "completion", "help", "version", "set", "get":
		return true
	}
	return false
}

// printShellHelp lists the commands available inside the session.
func printShellHelp(out io.Writer, cfg Config) {
	dir := assets.Dir("", time.Now())

	fmt.Fprintf(out, "Links\n")
	fmt.Fprintf(out, "  <link>                  download the link\n")
	fmt.Fprintf(out, "  <link> <link> ...       download several links in parallel\n")
	fmt.Fprintf(out, "  -d <link>,<link>        same, with the download flag\n")
	fmt.Fprintf(out, "  resolve <link>          show streams without downloading\n\n")

	fmt.Fprintf(out, "Options\n")
	fmt.Fprintf(out, "  -o <folder>             save into a folder (current: %s)\n", dir)
	fmt.Fprintf(out, "  -j <n>                  parallel downloads (default %d)\n", defaultJobs)
	fmt.Fprintf(out, "  --overwrite             replace an existing file\n\n")

	fmt.Fprintf(out, "Session\n")
	fmt.Fprintf(out, "  platforms               list supported platforms\n")
	fmt.Fprintf(out, "  set sessdata = \"<value>\"  store your Bilibili cookie for higher quality\n")
	fmt.Fprintf(out, "  set sessdata            show whether a cookie is stored\n")
	fmt.Fprintf(out, "  set sessdata =          remove the stored cookie\n")
	fmt.Fprintf(out, "  clear                   clear the screen\n")
	fmt.Fprintf(out, "  exit                    leave\n")
	fmt.Fprintf(out, "  a leading virelz or virelz.exe is ignored, so pasted commands work\n")
}
