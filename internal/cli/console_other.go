// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

//go:build !windows

package cli

// ensureConsole reports false because only Windows needs the extra step.
func ensureConsole() bool {
	return false
}
