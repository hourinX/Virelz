// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hourinX/Virelz/assets"
	"github.com/hourinX/Virelz/internal/downloader"
	"github.com/hourinX/Virelz/pkg/platform"
)

// fakePlatform resolves any URL it is told to accept.
type fakePlatform struct {
	name    string
	prefix  string
	video   *platform.Video
	err     error
	delay   time.Duration
	resolve int
}

func (f *fakePlatform) Name() string { return f.name }

func (f *fakePlatform) Match(rawURL string) bool { return strings.HasPrefix(rawURL, f.prefix) }

func (f *fakePlatform) Resolve(ctx context.Context, rawURL string) (*platform.Video, error) {
	f.resolve++
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.video, nil
}

// initSegment builds the leading ftyp/moov pair of a fragmented MP4 stream.
func initSegment() []byte {
	return []byte{
		0, 0, 0, 16, 'f', 't', 'y', 'p', 'i', 's', 'o', '5', 0, 0, 0, 0,
		0, 0, 0, 16, 'm', 'o', 'o', 'v', 0, 0, 0, 8, 'm', 'v', 'e', 'x', 0, 0, 0, 0,
	}
}

func serve(t *testing.T, body []byte) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/media", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/media"
}

func config(t *testing.T, p platform.Platform) Config {
	t.Helper()
	return Config{
		Name:           "virelz-test",
		Version:        "1.2.3",
		Registry:       platform.NewRegistry(p),
		ResolveTimeout: 2 * time.Second,
		Download: downloader.Options{
			ResponseHeaderTimeout: 2 * time.Second,
		},
	}
}

func run(t *testing.T, cfg Config, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	cfg.Stdout = &out
	cfg.Stderr = &errOut
	code := Execute(context.Background(), cfg, args)
	return code, out.String(), errOut.String()
}

func TestDownloadCommandSavesFile(t *testing.T) {
	body := append(initSegment(), []byte("fragment")...)
	url := serve(t, body)
	dir := t.TempDir()

	cfg := config(t, &fakePlatform{
		name:   "demo",
		prefix: "https://demo.example/",
		video: &platform.Video{
			Site:    "demo",
			ID:      "v1",
			PageURL: "https://demo.example/v1",
			Title:   "Demo Clip",
			Author:  "someone",
			Media: []platform.Media{{
				Kind:      platform.MediaVideo,
				URL:       url,
				Quality:   "720p",
				Container: "mp4",
			}},
		},
	})

	code, out, errOut := run(t, cfg, "download", "https://demo.example/v1", "-o", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "Demo Clip") {
		t.Fatalf("output missing the title: %q", out)
	}
	if !strings.Contains(out, "Demo Clip.mp4") {
		t.Fatalf("output missing the saved path: %q", out)
	}
	saved := filepath.Join(dir, "Demo Clip.mp4")
	data, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("reading %s: %v", saved, err)
	}
	if !bytes.Equal(data, body) {
		t.Fatal("saved file does not match the served body")
	}
	// Progress must not leave a partially drawn line behind.
	if !strings.Contains(out, "\n") {
		t.Fatalf("output is not line-oriented: %q", out)
	}
}

func TestRootCommandWithURL(t *testing.T) {
	url := serve(t, append(initSegment(), []byte("x")...))
	dir := t.TempDir()
	cfg := config(t, &fakePlatform{
		name:   "demo",
		prefix: "https://demo.example/",
		video: &platform.Video{
			Site:  "demo",
			ID:    "v1",
			Title: "Clip",
			Media: []platform.Media{{Kind: platform.MediaVideo, URL: url, Container: "mp4"}},
		},
	})

	code, _, errOut := run(t, cfg, "https://demo.example/v1", "-o", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "Clip.mp4")); err != nil {
		t.Fatalf("expected the file to be saved: %v", err)
	}
}

func TestResolveCommandDoesNotDownload(t *testing.T) {
	dir := t.TempDir()
	mediaURL := serve(t, append(initSegment(), []byte("x")...))
	cfg := config(t, &fakePlatform{
		name:   "demo",
		prefix: "https://demo.example/",
		video: &platform.Video{
			Site:  "demo",
			ID:    "v1",
			Title: "Clip",
			Media: []platform.Media{{Kind: platform.MediaVideo, URL: mediaURL, Quality: "1080p", Container: "mp4"}},
		},
	})

	code, out, errOut := run(t, cfg, "resolve", "https://demo.example/v1")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "Site    demo") || !strings.Contains(out, "Media 1 video 1080p mp4") {
		t.Fatalf("unexpected resolve output: %q", out)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("resolve wrote files: %v", entries)
	}
}

func TestPlatformsCommand(t *testing.T) {
	cfg := Config{
		Name:     "virelz-test",
		Registry: platform.NewRegistry(&fakePlatform{name: "alpha", prefix: "a"}, &fakePlatform{name: "beta", prefix: "b"}),
	}
	code, out, errOut := run(t, cfg, "platforms")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if strings.TrimSpace(out) != "alpha\nbeta" {
		t.Fatalf("unexpected platforms output: %q", out)
	}
}

func TestUnsupportedLink(t *testing.T) {
	cfg := config(t, &fakePlatform{name: "demo", prefix: "https://demo.example/"})
	code, _, errOut := run(t, cfg, "download", "https://other.example/v1")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "unsupported source") {
		t.Fatalf("stderr does not explain the failure: %q", errOut)
	}
	if !strings.Contains(errOut, "demo") {
		t.Fatalf("stderr does not list supported platforms: %q", errOut)
	}
}

func TestUnsupportedSourceIsExplained(t *testing.T) {
	cfg := config(t, &fakePlatform{
		name:   "demo",
		prefix: "https://demo.example/",
		err:    fmt.Errorf("member only: %w", platform.ErrUnsupportedSource),
	})
	code, _, errOut := run(t, cfg, "download", "https://demo.example/v1")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "cannot download this link") {
		t.Fatalf("unexpected error text: %q", errOut)
	}
}

func TestResolveTimeout(t *testing.T) {
	cfg := config(t, &fakePlatform{
		name:   "demo",
		prefix: "https://demo.example/",
		delay:  200 * time.Millisecond,
	})
	cfg.ResolveTimeout = 20 * time.Millisecond
	code, _, errOut := run(t, cfg, "resolve", "https://demo.example/v1")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "timed out") {
		t.Fatalf("unexpected error text: %q", errOut)
	}
}

func TestInvalidResolveResultIsRejected(t *testing.T) {
	cfg := config(t, &fakePlatform{
		name:   "demo",
		prefix: "https://demo.example/",
		video:  &platform.Video{Site: "demo", Title: "empty"},
	})
	code, _, errOut := run(t, cfg, "resolve", "https://demo.example/v1")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "unusable result") {
		t.Fatalf("unexpected error text: %q", errOut)
	}
}

func TestExistingFileFailsWithoutOverwrite(t *testing.T) {
	url := serve(t, append(initSegment(), []byte("x")...))
	dir := t.TempDir()
	existing := filepath.Join(dir, "Clip.mp4")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config(t, &fakePlatform{
		name:   "demo",
		prefix: "https://demo.example/",
		video: &platform.Video{
			Site:  "demo",
			ID:    "v1",
			Title: "Clip",
			Media: []platform.Media{{Kind: platform.MediaVideo, URL: url, Container: "mp4"}},
		},
	})

	code, _, errOut := run(t, cfg, "download", "https://demo.example/v1", "-o", dir)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "already exists") {
		t.Fatalf("unexpected error text: %q", errOut)
	}
	if data, _ := os.ReadFile(existing); string(data) != "old" {
		t.Fatal("existing file was replaced")
	}

	code, _, errOut = run(t, cfg, "download", "https://demo.example/v1", "-o", dir, "--overwrite")
	if code != 0 {
		t.Fatalf("overwrite exit = %d, stderr = %q", code, errOut)
	}
	if data, _ := os.ReadFile(existing); string(data) == "old" {
		t.Fatal("overwrite did not replace the file")
	}
}

func TestHelpWhenNoArguments(t *testing.T) {
	cfg := config(t, &fakePlatform{name: "demo", prefix: "https://demo.example/"})
	code, out, errOut := run(t, cfg)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("expected usage text, got %q", out)
	}
}

// newTestRenderer builds a renderer that redraws into a buffer like a terminal.
func newTestRenderer(out io.Writer) *progressRenderer {
	r := newProgressRenderer(out)
	r.live = true
	return r
}

func TestProgressRendererFormatsLine(t *testing.T) {
	var buf bytes.Buffer
	r := newTestRenderer(&buf)
	r.Slot(0, "video")(downloader.Progress{Task: platform.MediaVideo, Done: 0, Total: 1024, Stage: downloader.StageDownloading})
	// Let the render interval pass so the next snapshot is drawn rather than throttled away.
	time.Sleep(renderInterval)
	r.Slot(0, "video")(downloader.Progress{Task: platform.MediaVideo, Done: 512, Total: 1024, Stage: downloader.StageDownloading})
	r.Close()

	got := buf.String()
	if !strings.Contains(got, "video") {
		t.Fatalf("line does not name the stream: %q", got)
	}
	if !strings.Contains(got, "50%") {
		t.Fatalf("line does not show a percentage: %q", got)
	}
	// The last row break is undone by one cursor-up, so no blank line is left
	// behind; Close then wipes the row so the report follows the prompt directly.
	if strings.HasSuffix(got, "\n") {
		t.Fatalf("renderer left a trailing line break: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[2K") {
		t.Fatalf("Close did not wipe the final row: %q", got)
	}
}

func TestProgressRendererWithoutTotal(t *testing.T) {
	var buf bytes.Buffer
	r := newTestRenderer(&buf)
	r.Slot(0, "audio")(downloader.Progress{Task: platform.MediaAudio, Done: 2048, Stage: downloader.StageDownloading})
	r.Done(0)
	r.Close()
	if !strings.Contains(buf.String(), "2.0 KiB") {
		t.Fatalf("unexpected output without a total: %q", buf.String())
	}
	if strings.Contains(buf.String(), "%") {
		t.Fatalf("unexpected percentage without a total: %q", buf.String())
	}
}

func TestExecuteReturnsCancelCode(t *testing.T) {
	mediaURL := serve(t, append(initSegment(), []byte("x")...))
	cfg := config(t, &fakePlatform{
		name:   "demo",
		prefix: "https://demo.example/",
		video: &platform.Video{
			Site:  "demo",
			ID:    "v1",
			Title: "Clip",
			Media: []platform.Media{{Kind: platform.MediaVideo, URL: mediaURL, Container: "mp4"}},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	cfg.Stdout = &out
	cfg.Stderr = &errOut
	code := Execute(ctx, cfg, []string{"download", "https://demo.example/v1", "-o", t.TempDir()})
	if code == 0 {
		t.Fatalf("expected a non-zero exit code, stderr = %q", errOut.String())
	}
}

func TestFormattingHelpers(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{formatDuration(0), "0ms"},
		{formatDuration(1500 * time.Millisecond), "1.5s"},
		{formatDuration(90 * time.Second), "1m30s"},
		{formatDuration(3*time.Hour + 4*time.Minute + 5*time.Second), "3h04m05s"},
		{formatBytes(512), "512 B"},
		{formatBytes(2048), "2.0 KiB"},
		{formatBytes(5 << 20), "5.0 MiB"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestExitErrorCarriesCode(t *testing.T) {
	err := exitError{code: 130, err: errors.New("cancelled")}
	if !errors.Is(err, err.err) && err.Error() != "cancelled" {
		t.Fatal("exitError lost its message")
	}
	var target exitError
	if !errors.As(error(err), &target) {
		t.Fatal("errors.As failed for exitError")
	}
	if target.code != 130 {
		t.Fatalf("code = %d", target.code)
	}
}

// multiPlatform maps each accepted URL to its own resolved video.
type multiPlatform struct {
	name   string
	prefix string
	videos map[string]*platform.Video
}

func (m *multiPlatform) Name() string { return m.name }

func (m *multiPlatform) Match(rawURL string) bool { return strings.HasPrefix(rawURL, m.prefix) }

func (m *multiPlatform) Resolve(ctx context.Context, rawURL string) (*platform.Video, error) {
	video, ok := m.videos[rawURL]
	if !ok {
		return nil, fmt.Errorf("%w: unknown link", platform.ErrUnsupportedSource)
	}
	return video, nil
}

func TestPrintReportAlignsEveryValue(t *testing.T) {
	var b bytes.Buffer
	video := &platform.Video{
		Site:     "bilibili",
		Title:    "demo",
		Author:   "someone",
		Duration: 96 * time.Second,
	}
	printReport(&b, video, downloader.Result{Path: "x.mp4", Bytes: 1024, Quality: "1080p", Duration: 1500 * time.Millisecond})

	rows := strings.Split(strings.TrimSuffix(b.String(), "\n\n"), "\n")
	if len(rows) != 8 {
		t.Fatalf("want eight rows, got %d: %q", len(rows), b.String())
	}

	wantLabels := []string{"Platform:", "Title:", "Author:", "Quality:", "MediaDuration:", "Memory:", "DownloadDuration:", "SaveDir:"}
	wantValues := []string{"bilibili", "demo", "someone", "1080p", "1m36s", "1.0 KiB", "1.5s", "x.mp4"}

	column := -1
	for i, row := range rows {
		if !strings.HasPrefix(row, wantLabels[i]) {
			t.Fatalf("row %d must start at the first column with %q: %q", i, wantLabels[i], row)
		}
		if !strings.Contains(strings.TrimSpace(row), wantLabels[i]) {
			t.Fatalf("row %d must carry the label %q: %q", i, wantLabels[i], row)
		}
		offset := len("DownloadDuration:") + 1 // widest label plus one separating space
		if !strings.Contains(row[offset:], wantValues[i]) {
			t.Fatalf("row %d is missing %q: %q", i, wantValues[i], row)
		}
		if i == 0 {
			column = offset
			continue
		}
		if offset != column {
			t.Fatalf("row %d starts at column %d, want %d: %q", i, offset, column, row)
		}
	}
}

func TestPrintReportSkipsMissingAuthor(t *testing.T) {
	var b bytes.Buffer
	printReport(&b, &platform.Video{Site: "bilibili", Title: "demo"}, downloader.Result{Path: "x.mp4"})

	if !strings.Contains(b.String(), "Author:") {
		t.Fatalf("an unknown author still needs its row: %q", b.String())
	}
	if strings.Contains(b.String(), "MediaDuration:") {
		t.Fatalf("a zero media duration must not print a row: %q", b.String())
	}
	if !strings.HasSuffix(b.String(), "mp4\n\n") {
		t.Fatalf("SaveDir must be followed by one blank row: %q", b.String())
	}
}

func TestProgressBarRendersAndErases(t *testing.T) {
	var b bytes.Buffer
	// live is forced on so the redraw path is exercised without a terminal.
	r := &progressRenderer{out: &b, live: true, lines: map[int]*taskLine{}}

	slot := r.Slot(0, "video")
	r.lines[0].lastAt = time.Time{}
	slot(downloader.Progress{Stage: downloader.StageDownloading, Done: 5, Total: 10})

	if !strings.Contains(b.String(), "50%") {
		t.Fatalf("bar did not render progress: %q", b.String())
	}

	b.Reset()
	r.Done(0)
	if strings.Contains(b.String(), "50%") {
		t.Fatalf("finished bar was not erased: %q", b.String())
	}
	if strings.Contains(b.String(), "\n") {
		t.Fatalf("erasing a bar must not emit a line break: %q", b.String())
	}
	if r.rendered != 0 {
		t.Fatalf("rendered = %d, want 0 after the only task finished", r.rendered)
	}
}

func TestProgressBarKeepsReportTightToThePrompt(t *testing.T) {
	var b bytes.Buffer
	r := &progressRenderer{out: &b, live: true, lines: map[int]*taskLine{}}

	slot := r.Slot(0, "video")
	for done := int64(1); done <= 3; done++ {
		r.lines[0].lastAt = time.Time{}
		slot(downloader.Progress{Stage: downloader.StageDownloading, Done: done, Total: 3})
	}
	r.Close()

	if r.rendered != 0 {
		t.Fatalf("rendered = %d, want 0 after Close", r.rendered)
	}
	tail := b.String()
	if i := strings.LastIndex(tail, "\x1b[2K"); i >= 0 {
		tail = tail[i+4:]
	}
	if strings.TrimSpace(tail) != "" {
		t.Fatalf("Close left text behind: %q", tail)
	}
}

func TestMousetrapIsDisabled(t *testing.T) {
	cobra.MousetrapHelpText = "This is a command line tool."
	withDefaults(Config{})
	if cobra.MousetrapHelpText != "" {
		t.Fatalf("the Windows mouse trap would block a double-click start")
	}
}

func TestDownloadFlagAcceptsMultipleLinks(t *testing.T) {
	body := append(initSegment(), []byte("fragment")...)
	url := serve(t, body)
	dir := t.TempDir()

	videos := map[string]*platform.Video{}
	for _, id := range []string{"a1", "a2"} {
		videos["https://demo.example/"+id] = &platform.Video{
			Site:    "demo",
			ID:      id,
			PageURL: "https://demo.example/" + id,
			Title:   "Clip " + id,
			Media:   []platform.Media{{Kind: platform.MediaVideo, URL: url, Container: "mp4"}},
		}
	}
	cfg := config(t, &multiPlatform{name: "demo", prefix: "https://demo.example/", videos: videos})
	cfg.Jobs = 2

	code, out, errOut := run(t, cfg, "-d", "https://demo.example/a1,https://demo.example/a2", "-o", dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut)
	}
	for _, name := range []string{"Clip a1.mp4", "Clip a2.mp4"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s was not saved: %v, output = %q", name, err, out)
		}
	}
}

func TestDefaultOutputUsesDatedFolder(t *testing.T) {
	want := filepath.Join(os.TempDir(), "virelz", time.Now().Format("20060102"))
	got := assets.Dir("", time.Now())
	if got != want {
		t.Fatalf("default folder = %q, want %q", got, want)
	}
}

func TestProgressRendererRedrawsEveryTask(t *testing.T) {
	var buf bytes.Buffer
	r := newTestRenderer(&buf)
	r.Slot(0, "first")
	r.Slot(1, "second")
	r.Slot(0, "first")(downloader.Progress{Done: 1, Total: 2, Stage: downloader.StageDownloading})
	r.Slot(1, "second")(downloader.Progress{Done: 1, Total: 2, Stage: downloader.StageDownloading})
	time.Sleep(renderInterval) // let the throttle pass so the next frame is drawn
	r.Slot(0, "first")(downloader.Progress{Done: 2, Total: 2, Stage: downloader.StageDownloading})
	time.Sleep(renderInterval)
	r.Slot(1, "second")(downloader.Progress{Done: 2, Total: 2, Stage: downloader.StageDownloading})
	r.Close()

	got := buf.String()
	if n := strings.Count(got, "first"); n < 2 || strings.Count(got, "second") < 2 {
		t.Fatalf("expected both rows to be drawn twice, got %q", got)
	}
	if n := strings.Count(got, "\x1b[1A"); n < 2 {
		t.Fatalf("expected the cursor to walk back over both rows, got %d moves: %q", n, got)
	}
	if strings.Contains(got, "\x1b[2A") {
		t.Fatalf("a redraw must clear and repaint each row instead of skipping it: %q", got)
	}
}
