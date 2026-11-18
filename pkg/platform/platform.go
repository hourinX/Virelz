// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package platform

import (
	"context"
	"errors"
	"net/http"
	"time"
)

var ErrUnsupportedSource = errors.New("unsupported source") // link cannot be downloaded by design

// MediaKind describes what a media stream contains.
type MediaKind string

const (
	MediaVideo MediaKind = "video" // video stream, may be muxed with audio
	MediaAudio MediaKind = "audio" // audio-only stream from a split DASH source
)

// Media is one downloadable stream of a resolved video.
type Media struct {
	Kind      MediaKind   // what the stream contains
	URL       string      // media address, possibly short-lived
	Quality   string      // short label such as 720p
	Container string      // fragment container family, such as mp4
	Headers   http.Header // extra request headers the source requires
}

// Video is the result of resolving one source link.
type Video struct {
	Site     string        // platform key that produced the result
	ID       string        // platform-native identifier
	PageURL  string        // normalized page address
	Title    string        // title with filesystem-unsafe characters removed
	Author   string        // uploader name when the source reports one
	Duration time.Duration // media length when the source reports one
	Media    []Media       // streams that make up the download
}

// Platform resolves source links into downloadable media.
type Platform interface {
	Name() string             // platform key used in output and registry lookups
	Match(rawURL string) bool // reports whether the link belongs here, no network access
	Resolve(ctx context.Context, rawURL string) (*Video, error)
}

// Validate reports whether v can be handed to the downloader.
func (v *Video) Validate() error {
	if v == nil {
		return errors.New("resolve returned no video")
	}
	if len(v.Media) == 0 {
		return errors.New("resolve returned no media")
	}
	if len(v.Media) > 2 {
		return errors.New("resolve returned more than one video and one audio stream")
	}

	var video, audio int
	for _, m := range v.Media {
		if m.URL == "" {
			return errors.New("media stream has an empty URL")
		}
		switch m.Kind {
		case MediaVideo:
			video++
		case MediaAudio:
			audio++
		default:
			return errors.New("media stream has an unknown kind")
		}
	}

	if video > 1 || audio > 1 {
		return errors.New("resolve returned duplicate media streams")
	}
	if video == 0 {
		return errors.New("resolve returned no video stream")
	}
	if audio == 1 && v.Media[0].Container != v.Media[len(v.Media)-1].Container {
		return errors.New("media streams use different containers")
	}
	return nil
}
