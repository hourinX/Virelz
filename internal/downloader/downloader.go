// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hourinX/Virelz/pkg/platform"
)

// Options control one download run.
type Options struct {
	Dir                   string         // destination directory, created when missing
	Overwrite             bool           // replace an existing file instead of failing
	ResponseHeaderTimeout time.Duration  // bound for waiting on response headers only
	Progress              func(Progress) // receives byte counts as they are written
	Client                *http.Client   // overrides the HTTP client, used by tests
}

// Progress is a snapshot of a running transfer.
type Progress struct {
	Task  platform.MediaKind // stream being transferred
	Done  int64              // body bytes written so far
	Total int64              // expected final size, zero when unknown
	Stage Stage              // preparation or transfer
}

// Stage describes what a progress update refers to.
type Stage string

const (
	StageResolving   Stage = "resolving"   // metadata is being read from the platform
	StageDownloading Stage = "downloading" // body bytes are being written
	StageMerging     Stage = "merging"     // streams are being combined into one file
)

// Result describes a finished download.
type Result struct {
	Path     string        // published file
	Bytes    int64         // body bytes written for all streams
	Quality  string        // label reported by the platform
	Duration time.Duration // how long the transfer took
}

const DefaultResponseHeaderTimeout = 30 * time.Second // used when Options leaves it unset

// Download transfers every stream of video into Options.Dir and publishes the
// file only after all bytes are written. It works for any platform.Video, so
// platform-specific behavior belongs in the adapter, not here.
func Download(ctx context.Context, video *platform.Video, opts Options) (Result, error) {
	if err := video.Validate(); err != nil {
		return Result{}, err
	}

	opts = opts.withDefaults()
	client := opts.httpClient()

	// Prepare the destination and refuse to overwrite unless allowed.
	final, err := prepareDestination(video, opts)
	if err != nil {
		return Result{}, err
	}

	// Write into a private temporary file so a failed run never looks finished.
	tmp, err := os.CreateTemp(opts.Dir, ".virelz-*.part")
	if err != nil {
		return Result{}, fmt.Errorf("creating temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	published := false
	defer func() {
		if !published {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	// Transfer every stream into the temporary file.
	start := time.Now()
	total := sumSizes(ctx, client, video.Media)
	transferred, written, err := transferStreams(ctx, client, tmp, video.Media, total, opts)
	if err != nil {
		return Result{}, err
	}
	if transferred != len(video.Media) {
		return Result{}, fmt.Errorf("expected %d streams, transferred %d", len(video.Media), transferred)
	}

	// Flush and publish only after the whole file is durable.
	if err := tmp.Sync(); err != nil {
		return Result{}, fmt.Errorf("flushing file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return Result{}, fmt.Errorf("closing file: %w", err)
	}
	if err := os.Rename(tmpPath, final); err != nil {
		return Result{}, fmt.Errorf("publishing %s: %w", final, err)
	}
	published = true

	return Result{
		Path:     final,
		Bytes:    written,
		Quality:  firstQuality(video.Media),
		Duration: time.Since(start),
	}, nil
}

// Filename builds the output file name for a resolved video. The platform ID is
// not appended, so the saved path stays as short as the title allows.
func Filename(video *platform.Video) string {
	base := strings.TrimSpace(video.Title)
	if base == "" {
		base = video.Site + "-video"
	}

	ext := ""
	for _, m := range video.Media {
		if m.Container != "" {
			ext = "." + normalizeContainer(m.Container)
			break
		}
	}
	return base + ext
}

// prepareDestination creates the target directory and returns the final path.
func prepareDestination(video *platform.Video, opts Options) (string, error) {
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}
	final := filepath.Join(opts.Dir, Filename(video))
	if opts.Overwrite {
		return final, nil
	}
	if _, err := os.Stat(final); err == nil {
		return "", fmt.Errorf("%s already exists (use --overwrite to replace it)", final)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("checking destination: %w", err)
	}
	return final, nil
}

// normalizeContainer maps source spellings onto a file extension.
func normalizeContainer(container string) string {
	container = strings.ToLower(strings.TrimSpace(container))
	switch container {
	case "mp4", "m4s", "fmp4":
		return "mp4"
	case "webm":
		return "webm"
	default:
		return container
	}
}

// firstQuality reports the video label, or nothing when the source omitted it.
func firstQuality(media []platform.Media) string {
	for _, m := range media {
		if m.Kind == platform.MediaVideo && m.Quality != "" {
			return m.Quality
		}
	}
	return ""
}

// withDefaults fills the fields a caller may legitimately leave empty.
func (o Options) withDefaults() Options {
	if o.Dir == "" {
		o.Dir = "."
	}
	if o.ResponseHeaderTimeout <= 0 {
		o.ResponseHeaderTimeout = DefaultResponseHeaderTimeout
	}
	return o
}

// httpClient returns the injected client or one bounded by the header timeout.
func (o Options) httpClient() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{Transport: newTransport(o.ResponseHeaderTimeout)}
}

// report forwards a progress snapshot when the caller registered a callback.
func (o Options) report(p Progress) {
	if o.Progress != nil {
		o.Progress(p)
	}
}
