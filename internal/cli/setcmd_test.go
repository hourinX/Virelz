// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package cli

import "testing"

func TestParseSetArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantKey string
		wantVal string
		wantHas bool
		wantErr bool
	}{
		{name: "query only", args: []string{"sessdata"}, wantKey: "sessdata", wantHas: false},
		{name: "spaced equals", args: []string{"sessdata", "=", "abc"}, wantKey: "sessdata", wantVal: "abc", wantHas: true},
		{name: "spaced empty", args: []string{"sessdata", "="}, wantKey: "sessdata", wantHas: true},
		{name: "joined equals", args: []string{"sessdata=abc"}, wantKey: "sessdata", wantVal: "abc", wantHas: true},
		{name: "joined empty", args: []string{"sessdata="}, wantKey: "sessdata", wantHas: true},
		{name: "keeps inner quotes stripped", args: []string{"sessdata", "=", `"abc"`}, wantKey: "sessdata", wantVal: "abc", wantHas: true},
		{name: "single quotes", args: []string{"sessdata", "=", "'abc'"}, wantKey: "sessdata", wantVal: "abc", wantHas: true},
		{name: "value with equals inside", args: []string{"sessdata=ab=cd"}, wantKey: "sessdata", wantVal: "ab=cd", wantHas: true},
		{name: "missing equals", args: []string{"sessdata", "abc"}, wantErr: true},
		{name: "no args", args: nil, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, value, hasValue, err := parseSetArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got key=%q value=%q has=%v", key, value, hasValue)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != c.wantKey || value != c.wantVal || hasValue != c.wantHas {
				t.Fatalf("got key=%q value=%q has=%v, want key=%q value=%q has=%v",
					key, value, hasValue, c.wantKey, c.wantVal, c.wantHas)
			}
		})
	}
}
