package cycles

import (
	"path/filepath"

	"github.com/RTwoStudio/orbit/internal/domain"
	"github.com/RTwoStudio/orbit/internal/exit"
	"github.com/RTwoStudio/orbit/internal/registry"
)

// workStub loads the cycles registry cache and returns the raw
// work.stub.md content plus the cached cycles manifest version (the value
// stamped into a new note's registry-version). An empty or corrupt cache is a
// registry_unreachable (4).
func (s *Store) workStub() (content, registryVersion string, err error) {
	cache, err := registry.CacheLoad(domain.Cycles)
	if err != nil {
		return "", "", exit.New(exit.RegistryUnreachable,
			"cycles registry cache is empty or corrupted — run: orbit cycles update", err.Error())
	}
	for _, e := range cache.Manifest.Files.Stubs {
		if filepath.Base(e.Path) == "work.stub.md" {
			return string(cache.Content[e.Path]), cache.Manifest.Version, nil
		}
	}
	return "", "", exit.New(exit.RegistryUnreachable,
		"cycles registry has no work.stub.md — run: orbit cycles update")
}

// cycleStub loads the cycles registry cache and returns the raw
// cycle.stub.md content plus the cached cycles manifest version (the value
// stamped into a new cycle note's registry-version). An empty or corrupt cache
// is a registry_unreachable (4).
func (s *Store) cycleStub() (content, registryVersion string, err error) {
	cache, err := registry.CacheLoad(domain.Cycles)
	if err != nil {
		return "", "", exit.New(exit.RegistryUnreachable,
			"cycles registry cache is empty or corrupted — run: orbit cycles install (or update)", err.Error())
	}
	for _, e := range cache.Manifest.Files.Stubs {
		if filepath.Base(e.Path) == "cycle.stub.md" {
			return string(cache.Content[e.Path]), cache.Manifest.Version, nil
		}
	}
	return "", "", exit.New(exit.RegistryUnreachable,
		"cycles registry has no cycle.stub.md — run: orbit cycles update")
}
