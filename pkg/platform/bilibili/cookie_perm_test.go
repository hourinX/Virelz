// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

//go:build !windows

package bilibili

import (
	"os"
	"testing"
)

// Windows does not model POSIX permission bits, so this check is Unix only.
func TestSavedCookieIsNotWorldReadable(t *testing.T) {
	tempConfig(t)
	if err := SaveCookies("secret"); err != nil {
		t.Fatalf("SaveCookies: %v", err)
	}
	info, err := os.Stat(CookiePath())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("cookie file mode = %v, want no group or other access", info.Mode().Perm())
	}
}
