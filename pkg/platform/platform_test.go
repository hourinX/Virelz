// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package platform

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func okVideo() *Video {
	return &Video{
		Site:    "test",
		ID:      "1",
		PageURL: "https://example.com/watch/1",
		Title:   "sample",
		Media: []Media{{
			Kind:      MediaVideo,
			URL:       "https://cdn.example.com/sample.mp4",
			Quality:   "720p",
			Container: "mp4",
		}},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		video   *Video
		wantErr bool
	}{
		{name: "single muxed stream", video: okVideo()},
		{
			name: "split video and audio",
			video: func() *Video {
				v := okVideo()
				v.Media = append(v.Media, Media{Kind: MediaAudio, URL: "https://cdn.example.com/sample.m4s", Container: "mp4"})
				return v
			}(),
		},
		{
			name: "container mismatch",
			video: func() *Video {
				v := okVideo()
				v.Media[0].Container = "mp4"
				v.Media = append(v.Media, Media{Kind: MediaAudio, URL: "https://cdn.example.com/sample.webm", Container: "webm"})
				return v
			}(),
			wantErr: true,
		},
		{
			name:    "nil video",
			video:   nil,
			wantErr: true,
		},
		{
			name:    "no media",
			video:   &Video{Site: "test"},
			wantErr: true,
		},
		{
			name: "empty URL",
			video: func() *Video {
				v := okVideo()
				v.Media[0].URL = ""
				return v
			}(),
			wantErr: true,
		},
		{
			name: "audio only",
			video: func() *Video {
				v := okVideo()
				v.Media = []Media{{Kind: MediaAudio, URL: "https://cdn.example.com/a.m4s", Container: "mp4"}}
				return v
			}(),
			wantErr: true,
		},
		{
			name: "duplicate video streams",
			video: func() *Video {
				v := okVideo()
				v.Media = append(v.Media, Media{Kind: MediaVideo, URL: "https://cdn.example.com/b.mp4", Container: "mp4"})
				return v
			}(),
			wantErr: true,
		},
		{
			name: "unknown kind",
			video: func() *Video {
				v := okVideo()
				v.Media[0].Kind = "subtitle"
				return v
			}(),
			wantErr: true,
		},
		{
			name: "too many streams",
			video: func() *Video {
				v := okVideo()
				v.Media = append(v.Media,
					Media{Kind: MediaAudio, URL: "https://cdn.example.com/a1.m4s", Container: "mp4"},
					Media{Kind: MediaAudio, URL: "https://cdn.example.com/a2.m4s", Container: "mp4"},
				)
				return v
			}(),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.video.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

type fakePlatform struct {
	name    string
	matches bool
}

func (f fakePlatform) Name() string { return f.name }
func (f fakePlatform) Match(string) bool {
	return f.matches
}
func (f fakePlatform) Resolve(context.Context, string) (*Video, error) {
	v := okVideo()
	v.Site = f.name
	return v, nil
}

func TestRegistryLookup(t *testing.T) {
	reg := NewRegistry(fakePlatform{name: "alpha", matches: false}, fakePlatform{name: "beta", matches: true})

	p, err := reg.Lookup("https://example.com/watch/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "beta" {
		t.Fatalf("got platform %q, want beta", p.Name())
	}

	if names := reg.Names(); len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Fatalf("unexpected names %v", names)
	}

	_, err = NewRegistry(fakePlatform{name: "alpha"}).Lookup("https://example.com/watch/1")
	if !errors.Is(err, ErrUnsupportedSource) {
		t.Fatalf("expected ErrUnsupportedSource, got %v", err)
	}
}

func TestMediaCarriesHeaders(t *testing.T) {
	// A resolved stream must be able to demand request headers so that shared
	// transfer code never has to know platform specifics.
	m := Media{Kind: MediaVideo, URL: "https://cdn.example.com/v.m4s", Container: "mp4"}
	m.Headers = http.Header{}
	m.Headers.Set("Referer", "https://example.com/")
	if got := m.Headers.Get("Referer"); got != "https://example.com/" {
		t.Fatalf("got referer %q", got)
	}
	v := okVideo()
	v.Media = []Media{m}
	if err := v.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
