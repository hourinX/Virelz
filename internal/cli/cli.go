// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/hourinX/Virelz/assets"
	"github.com/hourinX/Virelz/internal/downloader"
	"github.com/hourinX/Virelz/pkg/platform"
)

// Config supplies the collaborators a command needs.
type Config struct {
	Name           string             // command name used in help and errors
	Version        string             // reported by --version
	Registry       *platform.Registry // resolves source links to media
	ResolveTimeout time.Duration      // bound for metadata lookup
	Download       downloader.Options // transfer defaults
	Jobs           int                // parallel download tasks
	Args           []string           // command line arguments for one-shot mode
	Stdin          io.Reader          // defaults to the process stdin
	Stdout         io.Writer          // defaults to the process stdout
	Stderr         io.Writer          // defaults to the process stderr
}

// Execute runs the CLI and returns a process exit code.
func Execute(ctx context.Context, cfg Config, args []string) int {
	cfg = withDefaults(cfg)

	root := newRootCommand(cfg)
	if len(args) > 0 {
		root.SetArgs(args)
	}

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(cfg.Stderr, "%s: %v\n", cfg.Name, err)
		var exit exitError
		if errors.As(err, &exit) {
			return exit.code
		}
		return 1
	}

	return 0
}

// withDefaults fills in the shared configuration and disables the Cobra
// mouse trap so a double-click reaches the interactive prompt instead.
func withDefaults(cfg Config) Config {
	cobra.MousetrapHelpText = ""
	if cfg.Name == "" {
		cfg.Name = assets.AppName
	}
	if cfg.ResolveTimeout <= 0 {
		cfg.ResolveTimeout = 30 * time.Second
	}
	if cfg.Jobs <= 0 {
		cfg.Jobs = defaultJobs
	}
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	return cfg
}

const defaultJobs = 3 // parallel downloads when -j is not given

// exitError carries an exit code without printing the message twice.
type exitError struct {
	code int   // process exit code
	err  error // underlying cause
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

func newRootCommand(cfg Config) *cobra.Command {
	root := &cobra.Command{
		Use:           cfg.Name + " [flags]",
		Short:         "Resolve and download videos from supported platforms",
		Version:       cfg.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !cmd.Flags().Changed("download") {
				return cmd.Help()
			}
			return runBatch(cmd, cfg, args, optionsFromFlags(cmd, cfg))
		},
	}
	root.SetIn(cfg.Stdin)
	root.SetOut(cfg.Stdout)
	root.SetErr(cfg.Stderr)

	root.PersistentFlags().StringSliceP("download", "d", nil, "links to download, comma separated or repeated")
	root.PersistentFlags().StringP("output", "o", "", "destination folder, defaults to the temp dir under virelz/<date>")
	root.PersistentFlags().IntP("jobs", "j", defaultJobs, "number of links downloaded at the same time")
	root.PersistentFlags().Bool("overwrite", false, "replace an existing file instead of failing")

	root.AddCommand(newDownloadCommand(cfg))
	root.AddCommand(newResolveCommand(cfg))
	root.AddCommand(newPlatformsCommand(cfg))
	root.AddCommand(newSetCommand())
	root.AddCommand(newGetCommand())
	return root
}

func newDownloadCommand(cfg Config) *cobra.Command {
	return &cobra.Command{
		Use:   "download [flags]",
		Short: "Download videos from a supported platform",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !cmd.Flags().Changed("download") {
				return cmd.Help()
			}
			return runBatch(cmd, cfg, args, optionsFromFlags(cmd, cfg))
		},
	}
}

func newResolveCommand(cfg Config) *cobra.Command {
	return &cobra.Command{
		Use:   "resolve <link>",
		Short: "Show what a link resolves to without downloading it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			video, err := resolve(cmd.Context(), cfg, args[0])
			if err != nil {
				return err
			}
			printResolved(cmd.OutOrStdout(), video)
			return nil
		},
	}
}

func newPlatformsCommand(cfg Config) *cobra.Command {
	return &cobra.Command{
		Use:   "platforms",
		Short: "List the platforms this build supports",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			names := cfg.Registry.Names()
			if len(names) == 0 {
				fmt.Fprintln(out, "no platforms are available in this build")
				return nil
			}
			for _, name := range names {
				fmt.Fprintln(out, name)
			}
			return nil
		},
	}
}

type runOptions struct {
	output    string // destination override, empty means the default
	overwrite bool   // replace existing files
	jobs      int    // parallel task count
}

func optionsFromFlags(cmd *cobra.Command, cfg Config) runOptions {
	output, _ := cmd.Flags().GetString("output")
	jobs, _ := cmd.Flags().GetInt("jobs")
	return runOptions{
		output:    strings.TrimSpace(output),
		overwrite: mustBool(cmd, "overwrite") || cfg.Download.Overwrite,
		jobs:      jobs,
	}
}

func mustBool(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}

// collectLinks merges positional arguments with -d values, which may be given
// comma separated or repeated.
func collectLinks(cmd *cobra.Command, args []string) []string {
	fromFlag, _ := cmd.Flags().GetStringSlice("download")
	return append(append([]string{}, args...), fromFlag...)
}

// runBatch resolves every link, then downloads them with bounded parallelism.
func runBatch(cmd *cobra.Command, cfg Config, args []string, opts runOptions) error {
	links := collectLinks(cmd, args)
	links = splitAndTrim(links)
	if len(links) == 0 {
		return errors.New("no link was given")
	}

	out := cmd.OutOrStdout()
	dir := assets.Dir(opts.output, time.Now())
	jobs := opts.jobs
	if jobs <= 0 {
		jobs = cfg.Jobs
	}

	// Resolve every link before transferring, so a bad link fails fast.
	videos := make([]*platform.Video, len(links))
	for i, link := range links {
		video, err := resolve(cmd.Context(), cfg, link)
		if err != nil {
			return err
		}
		videos[i] = video
	}
	if jobs > len(videos) {
		jobs = len(videos)
	}

	renderer := newProgressRenderer(out)
	defer renderer.Close()

	results := make([]downloader.Result, len(videos))
	errs := make([]error, len(videos))
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup

	for i, video := range videos {
		wg.Add(1)
		go func(index int, video *platform.Video) {
			defer wg.Done()

			sem <- struct{}{} // bound the number of concurrent transfers
			defer func() { <-sem }()

			dopts := cfg.Download
			dopts.Dir = dir
			dopts.Overwrite = opts.overwrite
			dopts.Progress = renderer.Slot(index, taskLabel(index, len(videos), video))

			results[index], errs[index] = downloader.Download(cmd.Context(), video, dopts)
			if errs[index] != nil {
				renderer.Fail(index)
				return
			}
			renderer.Done(index)
		}(i, video)
	}
	wg.Wait()
	renderer.Close()

	return reportResults(out, links, videos, results, errs)
}

// summaryField is one row of the aligned result block.
type summaryField struct {
	label string // right-aligned column ending with a colon
	value string // left-aligned column
}

// printReport writes the aligned block for one finished download. Every label
// is padded to the widest one, so all values start in the same column.
func printReport(w io.Writer, video *platform.Video, result downloader.Result) {
	mediaDuration := ""
	if video.Duration > 0 {
		mediaDuration = formatDuration(video.Duration)
	}
	fields := []summaryField{
		{"Platform:", siteName(video.Site)},
		{"Title:", video.Title},
		{"Author:", video.Author},
		{"Quality:", result.Quality},
		{"MediaDuration:", mediaDuration},
		{"Memory:", formatBytes(result.Bytes)},
		{"DownloadDuration:", formatDuration(result.Duration)},
		{"SaveDir:", absPath(result.Path)},
	}

	width := 0
	for _, field := range fields {
		if n := len(field.label); n > width {
			width = n
		}
	}
	for _, field := range fields {
		if field.value == "" {
			if field.label == "Author:" {
				field.value = "unknown"
			} else {
				continue
			}
		}
		fmt.Fprintf(w, "%-*s %s\n", width, field.label, field.value)
	}
	fmt.Fprintln(w)
}

// siteName prefers the resolved site key and falls back to a readable label.
func siteName(site string) string {
	if site == "" {
		return "unknown"
	}
	return site
}

// absPath reports the absolute form of path, or path itself on failure.
func absPath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func taskLabel(index, total int, video *platform.Video) string {
	if total == 1 {
		return string(platform.MediaVideo)
	}
	return fmt.Sprintf("#%d %s", index+1, video.ID)
}

func splitAndTrim(links []string) []string {
	out := make([]string, 0, len(links))
	for _, raw := range links {
		for _, part := range strings.Split(raw, ",") {
			if link := strings.TrimSpace(part); link != "" {
				out = append(out, link)
			}
		}
	}
	return out
}

func reportResults(out io.Writer, links []string, videos []*platform.Video, results []downloader.Result, errs []error) error {
	var failed int
	var firstErr error
	for i, err := range errs {
		if err == nil {
			continue
		}
		failed++
		if firstErr == nil {
			firstErr = err
		}
		if errors.Is(err, context.Canceled) {
			fmt.Fprintf(out, "Cancelled %s\n", links[i])
			continue
		}
		fmt.Fprintf(out, "Failed    %s: %v\n", links[i], err)
	}

	// printReport already ends with one blank row, so blocks stay separated.
	for i, result := range results {
		if result.Path == "" {
			continue
		}
		printReport(out, videos[i], result)
	}

	// A single failure keeps its own message so the reason stays readable.
	if failed == 1 && len(errs) == 1 {
		if errors.Is(firstErr, context.Canceled) {
			return exitError{code: 130, err: errors.New("cancelled")}
		}
		return firstErr
	}
	if failed > 0 {
		return exitError{code: 1, err: fmt.Errorf("%d of %d downloads failed", failed, len(errs))}
	}
	return nil
}

// resolve looks up a platform and resolves the link within the configured timeout.
func resolve(ctx context.Context, cfg Config, rawURL string) (*platform.Video, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("no link was given")
	}

	p, err := cfg.Registry.Lookup(rawURL) // resolve through the platform registry
	if err != nil {
		names := strings.Join(cfg.Registry.Names(), ", ")
		if names == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w (supported: %s)", err, names)
	}

	resolveCtx, cancel := context.WithTimeout(ctx, cfg.ResolveTimeout)
	defer cancel()

	video, err := p.Resolve(resolveCtx, rawURL)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return nil, fmt.Errorf("%s lookup cancelled", p.Name())
		case errors.Is(err, context.DeadlineExceeded):
			return nil, fmt.Errorf("%s lookup timed out after %s", p.Name(), cfg.ResolveTimeout)
		case errors.Is(err, platform.ErrUnsupportedSource):
			return nil, fmt.Errorf("%s cannot download this link: %w", p.Name(), err)
		default:
			return nil, fmt.Errorf("%s: %w", p.Name(), err)
		}
	}

	if err := video.Validate(); err != nil {
		return nil, fmt.Errorf("%s returned an unusable result: %w", p.Name(), err)
	}
	return video, nil
}

func printResolved(w io.Writer, video *platform.Video) {
	fmt.Fprintf(w, "Site    %s\n", video.Site)
	fmt.Fprintf(w, "ID      %s\n", video.ID)
	fmt.Fprintf(w, "Title   %s\n", video.Title)
	if video.Author != "" {
		fmt.Fprintf(w, "Author  %s\n", video.Author)
	}
	if video.Duration > 0 {
		fmt.Fprintf(w, "Length  %s\n", formatDuration(video.Duration))
	}
	fmt.Fprintf(w, "Page    %s\n", video.PageURL)

	for i, m := range video.Media {
		quality := m.Quality
		if quality == "" {
			quality = "default"
		}
		container := m.Container
		if container == "" {
			container = "unknown"
		}
		fmt.Fprintf(w, "Media %d %s %s %s\n", i+1, m.Kind, quality, container)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	d = d.Round(100 * time.Millisecond)
	total := int(d.Seconds())
	if total >= 3600 {
		return fmt.Sprintf("%dh%02dm%02ds", total/3600, (total%3600)/60, total%60)
	}
	if total >= 60 {
		return fmt.Sprintf("%dm%02ds", total/60, total%60)
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}
