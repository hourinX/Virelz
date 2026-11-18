// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package bilibili

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	bvidPattern = regexp.MustCompile(`(?i)^BV[0-9A-Za-z]{10}$`) // page ids look like BV1xx411c7mD
	avidPattern = regexp.MustCompile(`^av(\d+)$`)               // legacy numeric ids
	epIDPattern = regexp.MustCompile(`(?i)^ep(\d+)$`)           // bangumi episode ids
	ssIDPattern = regexp.MustCompile(`(?i)^ss(\d+)$`)           // bangumi season ids
)

// videoRef is a parsed Bilibili video identifier.
type videoRef struct {
	bvid string // BV id when present
	aid  string // numeric av id when the link uses one
	epID string // bangumi episode id when the link is a play page
	ssID string // bangumi season id when the link is a season page
}

func (r videoRef) value() string {
	switch {
	case r.epID != "":
		return "ep" + r.epID
	case r.ssID != "":
		return "ss" + r.ssID
	case r.bvid != "":
		return r.bvid
	}
	return "av" + r.aid
}

// bangumi reports whether the reference names a season or an episode.
func (r videoRef) bangumi() bool { return r.epID != "" || r.ssID != "" }

// parseVideoID extracts a BV, av, ep or ss identifier from a page link or shared text.
func parseVideoID(rawURL string) (videoRef, bool) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return videoRef{}, false
	}

	candidates := []string{rawURL}
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		candidates = append(candidates, u.Path, u.RawQuery, u.Fragment)
	}

	for _, candidate := range candidates {
		if ref, ok := scanForID(candidate); ok {
			return ref, true
		}
	}
	return videoRef{}, false
}

// scanForID looks for one identifier inside an arbitrary fragment of text.
func scanForID(s string) (videoRef, bool) {
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		return !isIDChar(r)
	}) {
		if bvidPattern.MatchString(field) {
			return videoRef{bvid: normalizeBVID(field)}, true
		}
		if m := avidPattern.FindStringSubmatch(strings.ToLower(field)); m != nil {
			return videoRef{aid: m[1]}, true
		}
		if m := epIDPattern.FindStringSubmatch(strings.ToLower(field)); m != nil {
			return videoRef{epID: m[1]}, true
		}
		if m := ssIDPattern.FindStringSubmatch(strings.ToLower(field)); m != nil {
			return videoRef{ssID: m[1]}, true
		}
	}
	return videoRef{}, false
}

func isIDChar(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// normalizeBVID restores the documented BV prefix casing.
func normalizeBVID(s string) string {
	return "BV" + s[2:]
}
