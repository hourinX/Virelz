// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/hourinX/Virelz/pkg/platform"
)

// transferMerged joins split streams into one fragmented MP4. Every stream is
// opened first because the merged header needs all of them at once.
func transferMerged(ctx context.Context, client *http.Client, w io.Writer, media []platform.Media, total int64, opts Options) (int, int64, error) {
	headers := make([]*streamHeader, 0, len(media))
	bodies := make([]io.Closer, 0, len(media))
	defer func() {
		for _, b := range bodies {
			_ = b.Close()
		}
	}()

	for _, m := range media {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		body, err := openMedia(ctx, client, m)
		if err != nil {
			return 0, 0, fmt.Errorf("downloading %s stream: %w", m.Kind, err)
		}
		bodies = append(bodies, body)
		header, err := readStreamHeader(body)
		if err != nil {
			return 0, 0, fmt.Errorf("reading %s header: %w", m.Kind, err)
		}
		headers = append(headers, header)
	}

	// Write the combined header before any fragment.
	merged, err := buildMergedHeader(headers)
	if err != nil {
		return 0, 0, err
	}
	if _, err := w.Write(merged); err != nil {
		return 0, 0, err
	}

	// Copy the remaining fragments of each stream into the same file.
	counter := &writeCounter{dst: w, total: total, opts: opts}
	counter.done = int64(len(merged))
	var transferred int
	for i, m := range media {
		counter.task = m.Kind
		opts.report(Progress{Task: m.Kind, Done: counter.done, Total: total, Stage: StageDownloading})
		src := headers[i].fragments
		dst := uint32(i + 1)
		if err := copyFragments(counter, src, headers[i].trackIDs()[0], dst); err != nil {
			return transferred, counter.written(), fmt.Errorf("downloading %s stream: %w", m.Kind, err)
		}
		transferred++
	}

	opts.report(Progress{Task: media[len(media)-1].Kind, Done: counter.written(), Total: total, Stage: StageDownloading})
	return transferred, counter.written(), nil
}
