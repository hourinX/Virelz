// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hourinX/Virelz/pkg/platform/bilibili"
)

const (
	sessDataName = "sessdata" // setting key accepted by the set command
	biliSiteName = "bilibili" // platform that owns SESSDATA
)

// newSetCommand stores a platform credential so later runs reach higher quality.
func newSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> = \"<value>\"",
		Short: "Store a value such as sessdata, used by later downloads",
		Long: "Store a value used by later downloads.\n\n" +
			"  virelz set sessdata = \"<value>\"   save your Bilibili session cookie\n" +
			"  virelz set sessdata                show whether one is stored\n" +
			"  virelz set sessdata =              remove the stored value",
		Args:                  cobra.ArbitraryArgs,
		DisableFlagsInUseLine: true,
		SilenceUsage:          true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			key, value, hasValue, err := parseSetArgs(args)
			if err != nil {
				return err
			}
			if key != sessDataName {
				return fmt.Errorf("unknown setting %q, the only setting is %q", key, sessDataName)
			}
			if !hasValue {
				return printSessDataStatus(out)
			}
			if value == "" {
				if err := bilibili.ClearCookies(); err != nil {
					return err
				}
				fmt.Fprintln(out, "bilibili\tstored sessdata removed")
				return nil
			}
			if err := bilibili.SaveCookies(value); err != nil {
				return err
			}
			fmt.Fprintf(out, "bilibili\tsessdata saved to %s\n", bilibili.CookiePath())
			fmt.Fprintln(out, "later downloads pick the highest quality your account allows")
			return nil
		},
	}
	return cmd
}

// newGetCommand prints the current value of a stored setting.
func newGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:                   "get <key>",
		Short:                 "Show a stored value such as sessdata",
		Args:                  cobra.ExactArgs(1),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if args[0] != sessDataName {
				return fmt.Errorf("unknown setting %q, the only setting is %q", args[0], sessDataName)
			}
			return printSessDataStatus(out)
		},
	}
}

// parseSetArgs accepts "sessdata", 'sessdata = "value"' and "sessdata=value"
// after the shell has already removed the surrounding quotes.
func parseSetArgs(args []string) (key string, value string, hasValue bool, err error) {
	if len(args) == 0 {
		return "", "", false, fmt.Errorf("name a setting, for example: set %s = \"<value>\"", sessDataName)
	}
	key = strings.TrimSpace(args[0])
	rest := args[1:]
	joined := strings.TrimSpace(strings.Join(rest, " "))
	if strings.HasPrefix(key, sessDataName) {
		// Allow "sessdata=value" and "sessdata=" to reach the same path.
		if eq := strings.Index(key, "="); eq >= 0 {
			return sessDataName, strings.Trim(strings.TrimSpace(key[eq+1:])+joined, `"`), true, nil
		}
	}
	if len(rest) == 0 {
		return key, "", false, nil
	}
	if !strings.HasPrefix(joined, "=") {
		return "", "", false, fmt.Errorf("expected '=', got %q", joined)
	}
	value = strings.TrimSpace(strings.TrimPrefix(joined, "="))
	value = strings.Trim(value, `"'`)
	return key, value, true, nil
}

// printSessDataStatus reports whether a cookie is stored without printing it.
func printSessDataStatus(out io.Writer) error {
	if bilibili.HasCookie() {
		fmt.Fprintf(out, "bilibili\tsessdata stored at %s\n", bilibili.CookiePath())
		return nil
	}
	fmt.Fprintln(out, "bilibili\tsessdata not set, quality is limited to 720p")
	return nil
}
