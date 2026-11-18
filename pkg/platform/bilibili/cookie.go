// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package bilibili

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	cookieEnv     = "VIRELZ_BILI_SESSDATA" // one-off override, highest priority
	configDirName = "virelz"               // directory name under the user config root
	configName    = "bilibili.json"        // stored cookie file
	sessDataKey   = "sessdata"             // key inside the config file
	sessDataField = "SESSDATA"             // cookie name Bilibili expects
)

// Cookies loads the SESSDATA cookie used to unlock the higher quality list.
// The environment variable wins, then the stored config file.
func Cookies() string {
	if v := strings.TrimSpace(os.Getenv(cookieEnv)); v != "" {
		return normalizeSessData(v)
	}
	path, err := configPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return normalizeSessData(sessDataKeyFrom(data))
}

// SaveCookies stores the SESSDATA cookie so later runs keep the higher quality.
func SaveCookies(sessData string) error {
	value := normalizeSessData(sessData)
	if value == "" {
		return errors.New("SESSDATA is empty")
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	body := fmt.Sprintf("{\n  %q: %q\n}\n", sessDataKey, value)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("writing cookie file: %w", err)
	}
	return nil
}

// ClearCookies removes the stored cookie file.
func ClearCookies() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// CookiePath reports where the cookie file lives, for user-facing messages.
func CookiePath() string {
	path, err := configPath()
	if err != nil {
		return ""
	}
	return path
}

// HasCookie reports whether a SESSDATA value is available at all.
func HasCookie() bool { return Cookies() != "" }

// configRoot can be replaced by tests to keep the real config untouched.
var configRoot = os.UserConfigDir

// configPath resolves the per-user config file location.
func configPath() (string, error) {
	root, err := configRoot()
	if err != nil {
		return "", fmt.Errorf("locating the user config directory: %w", err)
	}
	return filepath.Join(root, configDirName, configName), nil
}

// normalizeSessData accepts a bare value or a pasted "SESSDATA=..." pair.
func normalizeSessData(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimSpace(strings.TrimPrefix(value, sessDataField+"="))
	value = strings.Trim(value, `"'`)
	// A SESSDATA value never contains spaces or semicolons; keep the first token
	// so pasting a whole cookie header still yields something usable.
	if i := strings.IndexAny(value, "; \t\r\n"); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

// sessDataKeyFrom reads the stored key without pulling in a JSON dependency
// for a single string field.
func sessDataKeyFrom(data []byte) string {
	text := string(data)
	needle := `"` + sessDataKey + `"`
	i := strings.Index(text, needle)
	if i < 0 {
		return ""
	}
	rest := text[i+len(needle):]
	rest = strings.TrimSpace(rest)
	rest = strings.TrimPrefix(rest, ":")
	rest = strings.TrimSpace(rest)
	start := strings.Index(rest, `"`)
	if start < 0 {
		return ""
	}
	rest = rest[start+1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
