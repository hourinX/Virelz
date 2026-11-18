// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hourinX/Virelz/pkg/platform"
)

// newTransport bounds header waits while leaving the body unlimited, because a
// whole-transfer deadline would fail large or slow downloads.
func newTransport(timeout time.Duration) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = timeout
	transport.IdleConnTimeout = 60 * time.Second
	return transport
}

// transferStreams downloads the streams of media into w and reports how many
// were transferred plus the bytes written.
func transferStreams(ctx context.Context, client *http.Client, w io.Writer, media []platform.Media, total int64, opts Options) (int, int64, error) {
	if len(media) == 1 {
		n, err := transferSingle(ctx, client, w, media[0], total, opts)
		if err != nil {
			return 0, n, fmt.Errorf("downloading %s stream: %w", media[0].Kind, err)
		}
		return 1, n, nil
	}
	return transferMerged(ctx, client, w, media, total, opts)
}

// transferSingle copies one complete stream, header included.
func transferSingle(ctx context.Context, client *http.Client, w io.Writer, m platform.Media, total int64, opts Options) (int64, error) {
	body, err := openMedia(ctx, client, m)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = body.Close()
	}()

	counter := &writeCounter{dst: w, total: total, task: m.Kind, opts: opts}
	opts.report(Progress{Task: m.Kind, Done: 0, Total: total, Stage: StageDownloading})
	if _, err := io.Copy(counter, body); err != nil {
		return counter.written(), err
	}

	// Report the exact final count so a renderer never stops mid-bar.
	opts.report(Progress{Task: m.Kind, Done: counter.written(), Total: total, Stage: StageDownloading})
	return counter.written(), nil
}

// openMedia performs the GET for one media stream.
func openMedia(ctx context.Context, client *http.Client, m platform.Media) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	for name, values := range m.Headers {
		req.Header.Del(name)
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// writeCounter reports progress from bytes actually written.
type writeCounter struct {
	dst   io.Writer          // destination file
	done  int64              // bytes written including any merged header
	total int64              // expected total, zero when unknown
	task  platform.MediaKind // stream currently writing
	opts  Options            // carries the progress callback
	last  time.Time          // time of the last report
	sum   int64              // bytes since the last report
}

// Write records the transferred bytes and throttles progress reports.
func (c *writeCounter) Write(p []byte) (int, error) {
	n, err := c.dst.Write(p)
	c.done += int64(n)
	c.sum += int64(n)
	if c.last.IsZero() || c.sum >= 256<<10 || time.Since(c.last) > 200*time.Millisecond {
		c.opts.report(Progress{Task: c.task, Done: c.done, Total: c.total, Stage: StageDownloading})
		c.last = time.Now()
		c.sum = 0
	}
	return n, err
}

func (c *writeCounter) written() int64 { return c.done }
