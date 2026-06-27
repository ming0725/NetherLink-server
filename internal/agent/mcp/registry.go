package mcp

import "sort"

type ManifestRegistry struct {
	servers map[string]ServerManifest
}

func NewManifestRegistry(manifests ...ServerManifest) *ManifestRegistry {
	registry := &ManifestRegistry{servers: map[string]ServerManifest{}}
	for _, manifest := range manifests {
		normalized := normalizeServerManifest(manifest)
		if normalized.Name == "" {
			continue
		}
		registry.servers[normalized.Name] = normalized
	}
	return registry
}

func (r *ManifestRegistry) Get(name string) (ServerManifest, bool) {
	if r == nil || name == "" {
		return ServerManifest{}, false
	}
	manifest, ok := r.servers[name]
	return manifest, ok
}

func (r *ManifestRegistry) List() []ServerManifest {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.servers))
	for name := range r.servers {
		names = append(names, name)
	}
	sort.Strings(names)
	manifests := make([]ServerManifest, 0, len(names))
	for _, name := range names {
		manifests = append(manifests, r.servers[name])
	}
	return manifests
}
