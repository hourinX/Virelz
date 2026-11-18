// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package bilibili

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hourinX/Virelz/pkg/platform"
)

const seasonJSON = `{
  "code": 0,
  "message": "success",
  "result": {
    "season_id": 122262,
    "title": "Sample Anime",
    "season_type": 1,
    "episodes": [
      {"id": 2436276, "title": "1", "long_title": "First", "bvid": "BV1okUYBkEgU", "cid": 34278015600},
      {"id": 2436277, "title": "2", "long_title": "Second", "bvid": "BV1qrUYB5Ejm", "cid": 34278018022}
    ]
  }
}`

const pgcPlayJSON = `{
  "code": 0,
  "message": "success",
  "result": {
    "quality": 64,
    "accept_quality": [64, 32, 16],
    "dash": {
      "video": [
        {"id": 16, "baseUrl": "https://cdn.example/360.m4s", "width": 480, "height": 360, "bandwidth": 100},
        {"id": 64, "baseUrl": "https://cdn.example/576-av1.m4s", "width": 768, "height": 576, "codecs": "av01.0.00M.10.0", "bandwidth": 100},
        {"id": 64, "baseUrl": "https://cdn.example/576-avc.m4s", "width": 768, "height": 576, "codecs": "avc1.64001F", "bandwidth": 90},
        {"id": 64, "baseUrl": "https://cdn.example/576-hevc.m4s", "width": 768, "height": 576, "codecs": "hev1.1.6.L120.90", "bandwidth": 95}
      ],
      "audio": [
        {"id": 30216, "baseUrl": "https://cdn.example/audio.m4s", "bandwidth": 50}
      ]
    }
  }
}`

// newBangumiClient serves the season and pgc endpoints for ep-backed links.
func newBangumiClient(t *testing.T, seasonBody, playBody string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/pgc/view/web/season", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(seasonBody))
	})
	mux.HandleFunc("/pgc/player/web/playurl", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			t.Errorf("pgc playurl is missing a Referer header")
		}
		if got := r.URL.Query().Get("qn"); got != maxQualityID {
			t.Errorf("pgc qn = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(playBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New()
	c.apiBase = srv.URL
	return c
}

func TestResolveBangumiPicksRequestedEpisode(t *testing.T) {
	c := newBangumiClient(t, seasonJSON, pgcPlayJSON)

	video, err := c.Resolve(context.Background(), "https://www.bilibili.com/bangumi/play/ep2436277")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if video.ID != "ep2436277" {
		t.Fatalf("id = %q, want ep2436277", video.ID)
	}
	if video.Title != "Sample Anime - Second" {
		t.Fatalf("title = %q", video.Title)
	}
}

func TestResolveBangumiSeasonLinkUsesFirstEpisode(t *testing.T) {
	c := newBangumiClient(t, seasonJSON, pgcPlayJSON)

	video, err := c.Resolve(context.Background(), "https://www.bilibili.com/bangumi/play/ss122262")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if video.ID != "ep2436276" {
		t.Fatalf("id = %q, want the first episode ep2436276", video.ID)
	}
}

func TestResolveBangumiPicksHighestQualityWithAVC(t *testing.T) {
	c := newBangumiClient(t, seasonJSON, pgcPlayJSON)

	video, err := c.Resolve(context.Background(), "https://www.bilibili.com/bangumi/play/ep2436276")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if len(video.Media) != 2 {
		t.Fatalf("media count = %d, want video plus audio", len(video.Media))
	}
	if got := video.Media[0].Quality; got != "576p" {
		t.Fatalf("quality = %q, want 576p from the real track height", got)
	}
	if got := video.Media[0].URL; got != "https://cdn.example/576-avc.m4s" {
		t.Fatalf("picked %q, want the AVC track at the top quality id", got)
	}
	if got := video.Media[1].Kind; got != platform.MediaAudio {
		t.Fatalf("second track kind = %q, want audio", got)
	}
}

func TestResolveBangumiReportsUnknownEpisode(t *testing.T) {
	c := newBangumiClient(t, seasonJSON, pgcPlayJSON)

	if _, err := c.Resolve(context.Background(), "https://www.bilibili.com/bangumi/play/ep999999"); err == nil {
		t.Fatal("expected an error for an episode outside the season")
	}
}

func TestResolveBangumiReportsAPIError(t *testing.T) {
	c := newBangumiClient(t, `{"code":-404,"message":"啥都木有"}`, pgcPlayJSON)

	if _, err := c.Resolve(context.Background(), "https://www.bilibili.com/bangumi/play/ep2436276"); err == nil {
		t.Fatal("expected an error when the season request fails")
	}
}
