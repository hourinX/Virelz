// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hourinX/Virelz/pkg/platform"
)

const probeTimeout = 15 * time.Second // bound for one size probe

// sumSizes asks the servers for content lengths, returning zero when any size
// is unknown so the display falls back to an indeterminate bar.
func sumSizes(ctx context.Context, client *http.Client, media []platform.Media) int64 {
	var total int64
	for _, m := range media {
		size, err := contentLength(ctx, client, m)
		if err != nil || size <= 0 {
			return 0
		}
		total += size
	}
	return total
}

// contentLength asks for a single byte and reads the full size back from the
// response. Servers that reject HEAD are still answered this way.
func contentLength(ctx context.Context, client *http.Client, m platform.Media) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-0")
	req.Header.Set("Accept-Encoding", "identity")
	for name, values := range m.Headers {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusPartialContent {
		if size := sizeFromContentRange(resp.Header.Get("Content-Range")); size > 0 {
			return size, nil
		}
		return 0, errors.New("partial response without a parsable Content-Range")
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("range probe returned HTTP %d", resp.StatusCode)
	}
	if size := resp.ContentLength; size > 1 {
		return size, nil
	}
	return 0, errors.New("server did not report a content length")
}

// sizeFromContentRange reads the total size out of a "bytes 0-0/1234" header.
func sizeFromContentRange(value string) int64 {
	slash := strings.LastIndexByte(value, '/')
	if slash < 0 {
		return 0
	}
	size, err := strconv.ParseInt(strings.TrimSpace(value[slash+1:]), 10, 64)
	if err != nil || size <= 0 {
		return 0
	}
	return size
}
