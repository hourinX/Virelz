// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package bilibili

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hourinX/Virelz/pkg/platform"
)

// dashTrack is one DASH track, whose address may use either spelling.
type dashTrack struct {
	ID        int    `json:"id"`
	BaseURL   string `json:"baseUrl"`
	BaseURLV2 string `json:"base_url"`
	Codecs    string `json:"codecs"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Bandwidth int    `json:"bandwidth"`
}

// UnmarshalJSON accepts the two spellings of the track address.
func (t *dashTrack) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID          int      `json:"id"`
		BaseURL     string   `json:"baseUrl"`
		BaseURLV2   string   `json:"base_url"`
		BackupURL   []string `json:"backupUrl"`
		BackupURLV2 []string `json:"backup_url"`
		Codecs      string   `json:"codecs"`
		Width       int      `json:"width"`
		Height      int      `json:"height"`
		Bandwidth   int      `json:"bandwidth"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.ID = raw.ID
	t.Codecs = raw.Codecs
	t.Width = raw.Width
	t.Height = raw.Height
	t.Bandwidth = raw.Bandwidth
	t.BaseURL = raw.BaseURL
	if t.BaseURL == "" {
		t.BaseURL = raw.BaseURLV2
	}
	t.BaseURLV2 = ""
	return nil
}

// pickDash chooses the highest video track and, for split sources, one audio track.
// The returned id is the quality id of the chosen video track.
func pickDash(videos, audios []dashTrack, headers http.Header) ([]platform.Media, string, int, bool) {
	video, ok := highestTrack(videos)
	if !ok || video.BaseURL == "" {
		return nil, "", 0, false
	}

	streams := []platform.Media{{
		Kind:      platform.MediaVideo,
		URL:       video.BaseURL,
		Quality:   qualityLabel(video),
		Container: "mp4",
		Headers:   headers.Clone(),
	}}

	if audio, ok := pickTrack(audios, func(dashTrack) bool { return true }); ok && audio.BaseURL != "" {
		streams = append(streams, platform.Media{
			Kind:      platform.MediaAudio,
			URL:       audio.BaseURL,
			Container: "mp4",
			Headers:   headers.Clone(),
		})
	}

	return streams, qualityLabel(video), video.ID, true
}

// highestTrack returns the best playable track: the highest quality id first,
// then the most compatible codec, then the highest bitrate.
func highestTrack(tracks []dashTrack) (dashTrack, bool) {
	var best dashTrack
	found := false
	for _, t := range tracks {
		if t.BaseURL == "" {
			continue
		}
		if !found || betterTrack(t, best) {
			best = t
			found = true
		}
	}
	return best, found
}

// betterTrack reports whether a ranks above b for the same or a higher quality.
func betterTrack(a, b dashTrack) bool {
	if a.ID != b.ID {
		return a.ID > b.ID
	}
	ar, br := codecRank(a.Codecs), codecRank(b.Codecs)
	if ar != br {
		return ar > br
	}
	return a.Bandwidth > b.Bandwidth
}

// codecRank prefers codecs that play everywhere, so a higher one is picked first.
func codecRank(codecs string) int {
	lower := strings.ToLower(codecs)
	switch {
	case strings.HasPrefix(lower, "avc1"):
		return 3
	case strings.HasPrefix(lower, "hvc1"), strings.HasPrefix(lower, "hev1"):
		return 2
	case strings.HasPrefix(lower, "av01"):
		return 1
	default:
		return 0
	}
}

// pickTrack returns the highest quality track accepted by keep.
func pickTrack(tracks []dashTrack, keep func(dashTrack) bool) (dashTrack, bool) {
	filtered := make([]dashTrack, 0, len(tracks))
	for _, t := range tracks {
		if keep(t) {
			filtered = append(filtered, t)
		}
	}
	return highestTrack(filtered)
}

// qualityLabel turns a track into the short label shown in the report.
func qualityLabel(t dashTrack) string {
	if t.Height > 0 {
		return fmt.Sprintf("%dp", t.Height)
	}
	return qualityName(t.ID)
}

// qualityName maps a Bilibili quality id to the label shown in the report.
func qualityName(id int) string {
	switch id {
	case 127:
		return "4320p"
	case 126:
		return "2160p"
	case 125:
		return "2160p60"
	case 120:
		return "2160p"
	case 116:
		return "1080p60"
	case 112:
		return "1080p+"
	case 80:
		return "1080p"
	case 74:
		return "720p60"
	case 64:
		return "720p"
	case 32:
		return "480p"
	case 16:
		return "360p"
	default:
		return "unknown"
	}
}
