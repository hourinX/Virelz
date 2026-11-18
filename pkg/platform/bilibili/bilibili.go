// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package bilibili

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/hourinX/Virelz/pkg/platform"
)

const (
	siteName         = "bilibili"       // platform key used in output and lookups
	apiTimeout       = 20 * time.Second // bound for metadata and playurl calls
	apiBaseDefault   = "https://api.bilibili.com"
	maxQualityID     = "127" // highest quality id Bilibili accepts, so nothing caps the list
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// Client resolves Bilibili pages and must be built with New.
type Client struct {
	httpClient *http.Client  // connection pool with bounded header wait
	apiBase    string        // API host, overridden by tests
	userAgent  string        // UA sent with every API and media request
	apiTimeout time.Duration // deadline applied to resolution only
	sessData   string        // optional login cookie that unlocks the higher qualities
}

// New returns a Bilibili adapter with bounded connection timeouts.
func New() *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &Client{
		httpClient: &http.Client{Transport: transport},
		apiBase:    apiBaseDefault,
		userAgent:  defaultUserAgent,
		apiTimeout: apiTimeout,
		sessData:   Cookies(),
	}
}

// Name reports the platform key.
func (c *Client) Name() string { return siteName }

// Match reports whether the link carries a BV or av id, without network access.
func (c *Client) Match(rawURL string) bool {
	_, ok := parseVideoID(rawURL)
	return ok
}

// Resolve fetches metadata and playable streams for one Bilibili link.
func (c *Client) Resolve(ctx context.Context, rawURL string) (*platform.Video, error) {
	id, ok := parseVideoID(rawURL)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not a Bilibili video link", platform.ErrUnsupportedSource, rawURL)
	}

	ctx, cancel := context.WithTimeout(ctx, c.apiTimeout)
	defer cancel()

	if id.bangumi() {
		return c.resolveBangumi(ctx, id)
	}

	meta, err := c.fetchView(ctx, id)
	if err != nil {
		return nil, err
	}

	streams, quality, err := c.fetchStreams(ctx, meta)
	if err != nil {
		return nil, err
	}

	video := &platform.Video{
		Site:     siteName,
		ID:       meta.Data.BVID,
		PageURL:  meta.pageURL(),
		Title:    sanitizeTitle(meta.Data.Title),
		Author:   meta.Data.Owner.Name,
		Duration: time.Duration(meta.Data.Duration) * time.Second,
		Media:    streams,
	}
	for i := range streams {
		if streams[i].Kind == platform.MediaVideo {
			streams[i].Quality = quality
		}
	}
	if err := video.Validate(); err != nil {
		return nil, fmt.Errorf("resolving %s: %w", id.value(), err)
	}
	return video, nil
}
