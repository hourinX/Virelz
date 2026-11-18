// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package platform

import "fmt"

// Registry holds the platform adapters available to this build.
type Registry struct {
	platforms []Platform // adapters in match order
}

// NewRegistry builds a registry from the given adapters, keeping their order.
func NewRegistry(platforms ...Platform) *Registry {
	return &Registry{platforms: platforms}
}

// Lookup returns the first platform that matches rawURL.
func (r *Registry) Lookup(rawURL string) (Platform, error) {
	for _, p := range r.platforms {
		if p.Match(rawURL) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("%w: no supported platform matches this link", ErrUnsupportedSource)
}

// Names lists the registered platform keys in registration order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.platforms))
	for _, p := range r.platforms {
		names = append(names, p.Name())
	}
	return names
}
