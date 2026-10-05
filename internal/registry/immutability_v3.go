package registry

import (
	"fmt"
	"path/filepath"
	"strings"
)

// CheckImmutabilityV3 enforces append-only release history for the canonical
// v3 plugin sources. Older v2 source objects are accepted from the base ref so
// an established catalog can migrate formats without rewriting release bytes.
func CheckImmutabilityV3(repoDir, baseRef, pluginsDir string) error {
	if strings.TrimSpace(baseRef) == "" {
		return fmt.Errorf("base Git ref is required")
	}
	if !filepath.IsAbs(pluginsDir) {
		pluginsDir = filepath.Join(repoDir, pluginsDir)
	}
	current, err := LoadPluginsV3(pluginsDir)
	if err != nil {
		return err
	}
	basePaths, err := gitOutput(repoDir, "ls-tree", "-r", "--name-only", baseRef, "--", "plugins")
	if err != nil {
		return fmt.Errorf("read immutability base ref: %w", err)
	}
	base := map[string]Plugin{}
	for _, path := range strings.Split(strings.TrimSpace(string(basePaths)), "\n") {
		if path == "" || !strings.HasSuffix(path, ".json") {
			continue
		}
		data, e := gitOutput(repoDir, "show", baseRef+":"+path)
		if e != nil {
			return fmt.Errorf("read base plugin source: %w", e)
		}
		p, e := decodeBasePlugin(data)
		if e != nil {
			return fmt.Errorf("base plugin source is invalid: %w", e)
		}
		base[p.ID] = p
	}
	now := make(map[string]Plugin, len(current))
	for _, p := range current {
		now[p.ID] = Plugin{ID: p.ID, Type: p.Type, Name: p.Name, Repository: p.Repository, Channels: p.Channels, Releases: p.Releases}
	}
	for id, oldPlugin := range base {
		newPlugin, exists := now[id]
		if !exists {
			return fmt.Errorf("plugin %s was removed; releases are append-only", id)
		}
		oldReleases := releaseIdentities(oldPlugin)
		newReleases := releaseIdentities(newPlugin)
		for key, old := range oldReleases {
			fresh, ok := newReleases[key]
			if !ok {
				return fmt.Errorf("approved release identity %s was removed", key)
			}
			if old != fresh {
				return fmt.Errorf("approved release identity %s was modified", key)
			}
		}
	}
	return nil
}

func decodeBasePlugin(data []byte) (Plugin, error) {
	if p, err := ValidateSourceV3(data); err == nil {
		return Plugin{ID: p.ID, Type: p.Type, Name: p.Name, Repository: p.Repository, Channels: p.Channels, Releases: p.Releases}, nil
	}
	return ValidateSource(data)
}
