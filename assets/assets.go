// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package assets

import (
	"os"
	"path/filepath"
	"time"
)

const Version = "0.1.0" // current release version

const AppName = "virelz" // folder name and command name

const DateLayout = "20060102" // per-day output folder, local time

// Root returns the fixed output root inside the system temp directory.
func Root() string {
	return filepath.Join(os.TempDir(), AppName)
}

// Dir returns the output directory, falling back to root under one folder per day.
func Dir(override string, now time.Time) string {
	if override != "" {
		return override
	}
	return filepath.Join(Root(), now.Format(DateLayout))
}
