// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package bilibili

import (
	"fmt"
	"strings"
)

// safeMessage keeps API diagnostics short and free of echoed URLs.
func safeMessage(code int, message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Sprintf("code %d", code)
	}
	if len(message) > 120 {
		message = message[:120]
	}
	return fmt.Sprintf("code %d, %s", code, message)
}

// sanitizeTitle turns a source title into a filesystem-safe name.
func sanitizeTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "bilibili-video"
	}
	var b strings.Builder
	for _, r := range title {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteRune('_')
		default:
			if r < 0x20 {
				continue
			}
			b.WriteRune(r)
		}
	}
	cleaned := strings.Trim(b.String(), " .")
	if cleaned == "" {
		return "bilibili-video"
	}
	runes := []rune(cleaned)
	if len(runes) > 120 {
		cleaned = string(runes[:120])
	}
	return cleaned
}
