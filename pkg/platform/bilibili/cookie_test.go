// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package bilibili

import (
	"path/filepath"
	"testing"
)

func TestNormalizeSessData(t *testing.T) {
	cases := map[string]string{
		"abc123":                   "abc123",
		"SESSDATA=abc123":          "abc123",
		"  SESSDATA=abc123  ":      "abc123",
		`"abc123"`:                 "abc123",
		"SESSDATA=abc123; other=x": "abc123",
		"abc123; buvid3=x":         "abc123",
		"":                         "",
		"   ":                      "",
		"SESSDATA=":                "",
	}
	for in, want := range cases {
		if got := normalizeSessData(in); got != want {
			t.Errorf("normalizeSessData(%q) = %q, want %q", in, got, want)
		}
	}
}

// tempConfig points the cookie store at a throwaway directory.
func tempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := configRoot
	configRoot = func() (string, error) { return dir, nil }
	t.Cleanup(func() { configRoot = old })
	return dir
}

func TestSaveAndLoadCookies(t *testing.T) {
	dir := tempConfig(t)
	t.Setenv("VIRELZ_BILI_SESSDATA", "")

	if err := SaveCookies("SESSDATA=secret-value"); err != nil {
		t.Fatalf("SaveCookies: %v", err)
	}
	if got := Cookies(); got != "secret-value" {
		t.Fatalf("Cookies() = %q, want the saved value", got)
	}
	if path := CookiePath(); path != filepath.Join(dir, configDirName, configName) {
		t.Fatalf("CookiePath() = %q", path)
	}
	if err := ClearCookies(); err != nil {
		t.Fatalf("ClearCookies: %v", err)
	}
	if got := Cookies(); got != "" {
		t.Fatalf("Cookies() after clear = %q, want empty", got)
	}
	if err := ClearCookies(); err != nil {
		t.Fatalf("ClearCookies on a missing file: %v", err)
	}
}

func TestEnvironmentOverridesStoredCookie(t *testing.T) {
	tempConfig(t)
	if err := SaveCookies("stored-value"); err != nil {
		t.Fatalf("SaveCookies: %v", err)
	}
	t.Setenv("VIRELZ_BILI_SESSDATA", "env-value")
	if got := Cookies(); got != "env-value" {
		t.Fatalf("Cookies() = %q, want the environment value", got)
	}
}

func TestSaveCookiesRejectsEmptyValue(t *testing.T) {
	tempConfig(t)
	if err := SaveCookies("   "); err == nil {
		t.Fatal("expected an error for an empty SESSDATA")
	}
}

func TestClientSendsCookieWhenLoggedIn(t *testing.T) {
	c := New()
	c.sessData = "secret"
	headers := c.mediaHeaders()
	if got := headers.Get("Cookie"); got != "SESSDATA=secret" {
		t.Fatalf("mediaHeaders Cookie = %q", got)
	}
}

func TestClientOmitsCookieWhenAnonymous(t *testing.T) {
	c := New()
	c.sessData = ""
	if got := c.mediaHeaders().Get("Cookie"); got != "" {
		t.Fatalf("mediaHeaders sent a Cookie while logged out: %q", got)
	}
}
