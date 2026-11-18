// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package app

import (
	"context"
	"fmt"
	"time"

	"github.com/hourinX/Virelz/assets"
	"github.com/hourinX/Virelz/internal/cli"
	"github.com/hourinX/Virelz/internal/downloader"
	"github.com/hourinX/Virelz/pkg/platform"
	"github.com/hourinX/Virelz/pkg/platform/bilibili"
)

const Name = assets.AppName // executable name shown in help output

var version = assets.Version // overridable with -ldflags for custom builds

// NewRegistry builds the platform registry for this build.
func NewRegistry() *platform.Registry {
	return platform.NewRegistry(
		bilibili.New(),
	)
}

// Run starts the CLI, entering the interactive shell when launched without
// arguments, and returns the exit code.
func Run(ctx context.Context, args []string) int {
	return cli.Run(ctx, cli.Config{
		Name:           Name,
		Version:        version,
		Registry:       NewRegistry(),
		ResolveTimeout: 30 * time.Second,
		Download:       downloader.Options{},
		Args:           args,
	})
}

// ResolveOnly resolves a link without terminal rendering, for the future GUI.
func ResolveOnly(ctx context.Context, rawURL string, timeout time.Duration) (*platform.Video, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	p, err := NewRegistry().Lookup(rawURL) // resolve through the platform registry
	if err != nil {
		return nil, err
	}

	video, err := p.Resolve(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.Name(), err)
	}
	return video, nil
}
