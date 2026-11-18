// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package bilibili

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hourinX/Virelz/pkg/platform"
)

func TestMatch(t *testing.T) {
	c := New()
	accepted := []string{
		"https://www.bilibili.com/video/BV1GJ411x7h7",
		"https://www.bilibili.com/video/BV1GJ411x7h7/?spm_id_from=333.1007",
		"https://bilibili.com/video/bv1gj411x7h7",
		"https://m.bilibili.com/video/BV1GJ411x7h7?p=2",
		"https://www.bilibili.com/video/av80433022",
		"BV1GJ411x7h7",
		"【官方 MV】Never Gonna Give You Up https://www.bilibili.com/video/BV1GJ411x7h7 分享",
	}
	for _, in := range accepted {
		if !c.Match(in) {
			t.Errorf("expected %q to match", in)
		}
	}

	rejected := []string{
		"",
		"   ",
		"https://www.bilibili.com/",
		"https://www.bilibili.com/video/BV1GJ411x7",
		"https://www.bilibili.com/video/BV1GJ411x7h7x",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://x.com/i/status/1234567890",
		"BV",
		"not a link",
	}
	for _, in := range rejected {
		if c.Match(in) {
			t.Errorf("expected %q not to match", in)
		}
	}
}

func TestParseVideoID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://www.bilibili.com/video/BV1GJ411x7h7", "BV1GJ411x7h7"},
		{"https://www.bilibili.com/video/bv1gj411x7h7/?p=1", "BV1gj411x7h7"},
		{"https://www.bilibili.com/video/av80433022", "av80433022"},
		{"https://www.bilibili.com/bangumi/play/ep2436276?spm_id_from=333.337.0.0", "ep2436276"},
		{"https://www.bilibili.com/bangumi/play/ss122262", "ss122262"},
	}
	for _, tc := range tests {
		ref, ok := parseVideoID(tc.in)
		if !ok {
			t.Fatalf("expected %q to parse", tc.in)
		}
		if got := ref.value(); got != tc.want {
			t.Fatalf("%q parsed to %q, want %q", tc.in, got, tc.want)
		}
	}
}

const viewJSON = `{
  "code": 0,
  "message": "OK",
  "data": {
    "bvid": "BV1GJ411x7h7",
    "aid": 80433022,
    "title": "Sample: video / with unsafe chars?",
    "duration": 213,
    "pages": [{"cid": 137649199, "page": 1, "part": "main"}],
    "owner": {"name": "tester"}
  }
}`

const playurlDASH = `{
  "code": 0,
  "message": "OK",
  "data": {
    "dash": {
      "duration": 213,
      "video": [
        {"id": 16, "base_url": "https://cdn.example.com/360-hevc.m4s", "codecs": "hev1.1.6.L120.90", "width": 640, "height": 360, "bandwidth": 153220},
        {"id": 32, "baseUrl": "https://cdn.example.com/480-avc.m4s", "codecs": "avc1.64001F", "width": 852, "height": 480, "bandwidth": 786766},
        {"id": 32, "baseUrl": "https://cdn.example.com/480-hevc.m4s", "codecs": "hev1.1.6.L120.90", "width": 852, "height": 480, "bandwidth": 218550}
      ],
      "audio": [
        {"id": 30216, "base_url": "https://cdn.example.com/audio-low.m4s", "codecs": "mp4a.40.5", "bandwidth": 43962},
        {"id": 30280, "base_url": "https://cdn.example.com/audio-high.m4s", "codecs": "mp4a.40.2", "bandwidth": 203786}
      ]
    }
  }
}`

// newTestClient points the adapter at a stub API server.
func newTestClient(t *testing.T, viewBody, playurlBody string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			t.Errorf("view request is missing a Referer header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(viewBody))
	})
	mux.HandleFunc("/x/player/playurl", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("cid"); got != "137649199" {
			t.Errorf("playurl cid = %q", got)
		}
		if got := r.URL.Query().Get("fnval"); got != "4048" && got != "1" {
			t.Errorf("playurl fnval = %q", got)
		}
		if got := r.URL.Query().Get("qn"); got != maxQualityID {
			t.Errorf("playurl qn = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(playurlBody))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New()
	c.apiBase = srv.URL
	return c
}

func TestResolveDASHPrefersAVC(t *testing.T) {
	c := newTestClient(t, viewJSON, playurlDASH)

	video, err := c.Resolve(context.Background(), "https://www.bilibili.com/video/BV1GJ411x7h7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if video.Site != "bilibili" || video.ID != "BV1GJ411x7h7" {
		t.Fatalf("unexpected identity: %+v", video)
	}
	if video.Title != "Sample_ video _ with unsafe chars_" {
		t.Fatalf("title not sanitized: %q", video.Title)
	}
	if video.Author != "tester" || video.Duration != 213*time.Second {
		t.Fatalf("unexpected metadata: author=%q duration=%v", video.Author, video.Duration)
	}
	if video.PageURL != "https://www.bilibili.com/video/BV1GJ411x7h7" {
		t.Fatalf("unexpected page URL %q", video.PageURL)
	}
	if len(video.Media) != 2 {
		t.Fatalf("expected video and audio streams, got %+v", video.Media)
	}
	if video.Media[0].Kind != platform.MediaVideo || video.Media[0].URL != "https://cdn.example.com/480-avc.m4s" {
		t.Fatalf("unexpected video stream %+v", video.Media[0])
	}
	if video.Media[0].Quality != "480p" || video.Media[0].Container != "mp4" {
		t.Fatalf("unexpected video metadata %+v", video.Media[0])
	}
	if video.Media[1].Kind != platform.MediaAudio || video.Media[1].URL != "https://cdn.example.com/audio-high.m4s" {
		t.Fatalf("unexpected audio stream %+v", video.Media[1])
	}
	for _, m := range video.Media {
		if m.Headers.Get("Referer") != "https://www.bilibili.com/" {
			t.Fatalf("media %v is missing the required Referer", m.Kind)
		}
		if m.Headers.Get("User-Agent") == "" {
			t.Fatalf("media %v is missing a User-Agent", m.Kind)
		}
	}
}

func TestResolveFallsBackToSingleFile(t *testing.T) {
	const playurlSingle = `{"code":0,"message":"OK","data":{"durl":[{"url":"https://cdn.example.com/merged.mp4"}]}}`
	c := newTestClient(t, viewJSON, playurlSingle)

	video, err := c.Resolve(context.Background(), "https://www.bilibili.com/video/av80433022")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if video.ID != "BV1GJ411x7h7" {
		t.Fatalf("expected the BV id once metadata resolves, got %q", video.ID)
	}
	if len(video.Media) != 1 || video.Media[0].URL != "https://cdn.example.com/merged.mp4" {
		t.Fatalf("unexpected media %+v", video.Media)
	}
}

func TestResolveUnsupported(t *testing.T) {
	tests := []struct {
		name     string
		view     string
		play     string
		wantText string
	}{
		{
			name:     "removed video",
			view:     `{"code":-404,"message":"啥都木有"}`,
			play:     `{"code":0}`,
			wantText: "does not exist",
		},
		{
			name:     "access controlled",
			view:     `{"code":-10403,"message":"访问权限不足"}`,
			play:     `{"code":0}`,
			wantText: "unsupported source",
		},
		{
			name:     "no playable stream",
			view:     viewJSON,
			play:     `{"code":0,"message":"OK","data":{}}`,
			wantText: "not playable without signing in",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, tc.view, tc.play)
			_, err := c.Resolve(context.Background(), "https://www.bilibili.com/video/BV1GJ411x7h7")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, platform.ErrUnsupportedSource) {
				t.Fatalf("expected ErrUnsupportedSource, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("error %q does not mention %q", err, tc.wantText)
			}
		})
	}
}

func TestResolveRejectsForeignURLWithoutNetwork(t *testing.T) {
	c := New()
	c.apiBase = "http://127.0.0.1:1" // any request would fail loudly

	_, err := c.Resolve(context.Background(), "https://example.com/not-bilibili")
	if !errors.Is(err, platform.ErrUnsupportedSource) {
		t.Fatalf("expected ErrUnsupportedSource, got %v", err)
	}
}

func TestResolveHonoursCancellation(t *testing.T) {
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	c := New()
	c.apiBase = srv.URL
	c.apiTimeout = 50 * time.Millisecond

	start := time.Now()
	_, err := c.Resolve(context.Background(), "https://www.bilibili.com/video/BV1GJ411x7h7")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("resolution was not bounded by apiTimeout: %v", elapsed)
	}
	if errors.Is(err, platform.ErrUnsupportedSource) {
		t.Fatalf("a timeout must not be reported as an unsupported source: %v", err)
	}
}

func TestSanitizeTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "bilibili-video"},
		{"   ", "bilibili-video"},
		{"a/b\\c:d*e?f\"g<h>i|j", "a_b_c_d_e_f_g_h_i_j"},
		{"  trailing dots...  ", "trailing dots"},
		{"line\nbreak", "linebreak"},
	}
	for _, tc := range tests {
		if got := sanitizeTitle(tc.in); got != tc.want {
			t.Fatalf("sanitizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	long := strings.Repeat("长", 200)
	if got := []rune(sanitizeTitle(long)); len(got) > 120 {
		t.Fatalf("title was not truncated: %d runes", len(got))
	}
}

func TestHighestTrackPrefersTopQuality(t *testing.T) {
	headers := http.Header{}
	tracks := []dashTrack{
		{ID: 16, Height: 360, Codecs: "avc1.64001e", BaseURL: "u360", Bandwidth: 200000},
		{ID: 32, Height: 480, Codecs: "avc1.640033", BaseURL: "u480", Bandwidth: 350000},
		{ID: 120, Height: 2160, Codecs: "av01.0.12M.08", BaseURL: "u2160-av1", Bandwidth: 9000000},
		{ID: 80, Height: 1080, Codecs: "avc1.640028", BaseURL: "u1080", Bandwidth: 3000000},
		{ID: 120, Height: 2160, Codecs: "hvc1.2.4.L153", BaseURL: "u2160-hevc", Bandwidth: 7000000},
	}
	streams, quality, _, ok := pickDash(tracks, nil, headers)
	if !ok {
		t.Fatal("pickDash found no stream")
	}
	if quality != "2160p" {
		t.Fatalf("quality = %q, want 2160p", quality)
	}
	if streams[0].URL != "u2160-hevc" {
		t.Fatalf("picked %q, want the more compatible 4K track", streams[0].URL)
	}
}

func TestHighestTrackStepsDownWhenTopMissing(t *testing.T) {
	tracks := []dashTrack{
		{ID: 16, Height: 360, Codecs: "avc1", BaseURL: "u360"},
		{ID: 64, Height: 720, Codecs: "avc1", BaseURL: "u720"},
		{ID: 32, Height: 480, Codecs: "avc1", BaseURL: "u480"},
	}
	streams, quality, _, ok := pickDash(tracks, nil, http.Header{})
	if !ok {
		t.Fatal("pickDash found no stream")
	}
	if quality != "720p" || streams[0].URL != "u720" {
		t.Fatalf("quality = %q url = %q, want the best available 720p track", quality, streams[0].URL)
	}
}

func TestHighestTrackIgnoresTracksWithoutAddress(t *testing.T) {
	tracks := []dashTrack{
		{ID: 120, Height: 2160, Codecs: "avc1", BaseURL: ""},
		{ID: 32, Height: 480, Codecs: "avc1", BaseURL: "u480"},
	}
	streams, quality, _, ok := pickDash(tracks, nil, http.Header{})
	if !ok {
		t.Fatal("pickDash should skip the unusable 4K entry, not give up")
	}
	if quality != "480p" || streams[0].URL != "u480" {
		t.Fatalf("quality = %q url = %q", quality, streams[0].URL)
	}
}

func TestResolvePrefersHigherQualityAcrossEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(viewJSON))
	})
	mux.HandleFunc("/x/player/playurl", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("fnval") == "1" {
			// The MP4 endpoint answers 720p for the same clip.
			_, _ = w.Write([]byte(`{"code":0,"data":{"quality":64,"durl":[{"url":"https://cdn.example.com/720-mp4.mp4"}]}}`))
			return
		}
		// DASH only offers 480p here.
		_, _ = w.Write([]byte(`{"code":0,"data":{"dash":{"video":[{"id":32,"height":480,"codecs":"avc1.640033","baseUrl":"https://cdn.example.com/480-avc.m4s","bandwidth":354571}],"audio":[{"id":30280,"baseUrl":"https://cdn.example.com/audio.m4s"}]}}}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New()
	c.apiBase = srv.URL
	c.httpClient = srv.Client()

	video, err := c.Resolve(context.Background(), "https://www.bilibili.com/video/BV1GJ411x7h7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(video.Media) != 1 {
		t.Fatalf("expected the single-file 720p stream, got %+v", video.Media)
	}
	if video.Media[0].URL != "https://cdn.example.com/720-mp4.mp4" {
		t.Fatalf("picked %q, want the higher quality MP4 stream", video.Media[0].URL)
	}
	if video.Media[0].Quality != "720p" {
		t.Fatalf("quality = %q, want 720p", video.Media[0].Quality)
	}
}

func TestResolveKeepsDashWhenItIsBetter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(viewJSON))
	})
	mux.HandleFunc("/x/player/playurl", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("fnval") == "1" {
			_, _ = w.Write([]byte(`{"code":0,"data":{"quality":32,"durl":[{"url":"https://cdn.example.com/480-mp4.mp4"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"dash":{"video":[{"id":80,"height":1080,"codecs":"avc1.640028","baseUrl":"https://cdn.example.com/1080-avc.m4s","bandwidth":3000000}],"audio":[{"id":30280,"baseUrl":"https://cdn.example.com/audio.m4s"}]}}}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New()
	c.apiBase = srv.URL
	c.httpClient = srv.Client()

	video, err := c.Resolve(context.Background(), "https://www.bilibili.com/video/BV1GJ411x7h7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(video.Media) != 2 {
		t.Fatalf("expected split DASH streams, got %+v", video.Media)
	}
	if video.Media[0].URL != "https://cdn.example.com/1080-avc.m4s" || video.Media[0].Quality != "1080p" {
		t.Fatalf("unexpected video stream %+v", video.Media[0])
	}
}
