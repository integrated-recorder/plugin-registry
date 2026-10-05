package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const SchemaVersionV3 = 3

// CatalogV3 is the canonical publisher-aware catalog. Catalog remains the v2
// compatibility projection consumed by existing Core runtimes.
type CatalogV3 struct {
	SchemaVersion int        `json:"schema_version"`
	Plugins       []PluginV3 `json:"plugins"`
}

type PluginV3 struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Publisher  Publisher         `json:"publisher"`
	Name       string            `json:"name"`
	Repository string            `json:"repository"`
	Channels   map[string]string `json:"channels"`
	Releases   []Release         `json:"releases"`
}

type Publisher struct {
	Kind string `json:"kind"`
}

var sourceShapesV3 = map[string][]string{
	"":                       {"id", "type", "publisher", "name", "repository", "channels", "releases"},
	"publisher":              {"kind"},
	"channels":               {"stable", "beta", "development"},
	"releases[]":             {"version", "protocol", "source_commit", "artifacts"},
	"releases[].protocol":    {"name", "version"},
	"releases[].artifacts[]": {"os", "arch", "url", "filename", "size", "sha256"},
}

var catalogShapesV3 = map[string][]string{
	"":                                 {"schema_version", "plugins"},
	"plugins[]":                        {"id", "type", "publisher", "name", "repository", "channels", "releases"},
	"plugins[].publisher":              {"kind"},
	"plugins[].channels":               {"stable", "beta", "development"},
	"plugins[].releases[]":             {"version", "protocol", "source_commit", "artifacts"},
	"plugins[].releases[].protocol":    {"name", "version"},
	"plugins[].releases[].artifacts[]": {"os", "arch", "url", "filename", "size", "sha256"},
}

// LoadPluginsV3 loads the one-plugin-per-file canonical v3 source tree.
func LoadPluginsV3(dir string) ([]PluginV3, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read plugins directory: %w", err)
	}
	plugins := make([]PluginV3, 0, len(entries))
	for _, ent := range entries {
		name := ent.Name()
		if name == ".gitkeep" {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			return nil, fmt.Errorf("unexpected file in plugins directory: %s", safeName(name))
		}
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect plugin file: %w", err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > MaxCatalogBytes {
			return nil, fmt.Errorf("plugin source file is not a bounded regular file: %s", safeName(name))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read plugin source: %w", err)
		}
		p, err := ValidateSourceV3(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", safeName(name), err)
		}
		wantName := p.Type + "." + p.ID + ".json"
		if name != wantName {
			return nil, fmt.Errorf("plugin source filename must be %s", wantName)
		}
		plugins = append(plugins, p)
	}
	if err := ValidateV3(CatalogV3{SchemaVersion: SchemaVersionV3, Plugins: plugins}); err != nil {
		return nil, err
	}
	if _, err := BuildV3(plugins); err != nil {
		return nil, err
	}
	if _, err := BuildV2Projection(plugins); err != nil {
		return nil, err
	}
	return plugins, nil
}

func ValidateSourceV3(data []byte) (PluginV3, error) {
	var p PluginV3
	if err := decodeExact(data, sourceShapesV3, &p); err != nil {
		return PluginV3{}, err
	}
	if err := ValidateV3(CatalogV3{SchemaVersion: SchemaVersionV3, Plugins: []PluginV3{p}}); err != nil {
		return PluginV3{}, err
	}
	return p, nil
}

func DecodeCatalogV3(data []byte) (CatalogV3, error) {
	var c CatalogV3
	if err := decodeExact(data, catalogShapesV3, &c); err != nil {
		return CatalogV3{}, err
	}
	if err := ValidateV3(c); err != nil {
		return CatalogV3{}, err
	}
	return c, nil
}

func ValidateV3(c CatalogV3) error {
	if c.SchemaVersion != SchemaVersionV3 {
		return errors.New("catalog schema_version must be 3")
	}
	if len(c.Plugins) > MaxPlugins {
		return errors.New("catalog exceeds plugin limit")
	}
	seen := make(map[string]bool, len(c.Plugins))
	v2 := Catalog{SchemaVersion: SchemaVersion, Plugins: make([]Plugin, 0, len(c.Plugins))}
	for _, p := range c.Plugins {
		if seen[p.ID] {
			return fmt.Errorf("duplicate plugin ID %q", p.ID)
		}
		seen[p.ID] = true
		if p.Publisher.Kind != "first_party" && p.Publisher.Kind != "third_party" {
			return fmt.Errorf("plugin %s publisher.kind must be first_party or third_party", p.ID)
		}
		if p.Type == "source" && p.ID == "hls" {
			return errors.New("source plugin ID hls is reserved")
		}
		if p.Publisher.Kind == "first_party" && !isFirstPartyRepository(p.Repository) {
			return fmt.Errorf("first-party plugin %s repository must belong to github.com/integrated-recorder", p.ID)
		}
		v2.Plugins = append(v2.Plugins, Plugin{
			ID: p.ID, Type: p.Type, Name: p.Name, Repository: p.Repository,
			Channels: cloneChannels(p.Channels), Releases: cloneReleases(p.Releases),
		})
	}
	return Validate(v2)
}

// V2Projection drops only v3 publisher metadata, preserving the v2 wire shape
// for existing Core deployments during catalog migration.
func V2Projection(plugins []PluginV3) []Plugin {
	out := make([]Plugin, 0, len(plugins))
	for _, p := range plugins {
		out = append(out, Plugin{
			ID: p.ID, Type: p.Type, Name: p.Name, Repository: p.Repository,
			Channels: cloneChannels(p.Channels), Releases: cloneReleases(p.Releases),
		})
	}
	return out
}

func BuildV3(plugins []PluginV3) ([]byte, error) {
	c := CatalogV3{SchemaVersion: SchemaVersionV3, Plugins: clonePluginsV3(plugins)}
	if err := ValidateV3(c); err != nil {
		return nil, err
	}
	sort.Slice(c.Plugins, func(i, j int) bool { return c.Plugins[i].ID < c.Plugins[j].ID })
	for i := range c.Plugins {
		sort.Slice(c.Plugins[i].Releases, func(a, b int) bool { return c.Plugins[i].Releases[a].Version < c.Plugins[i].Releases[b].Version })
		for j := range c.Plugins[i].Releases {
			sort.Slice(c.Plugins[i].Releases[j].Artifacts, func(a, b int) bool {
				l, r := c.Plugins[i].Releases[j].Artifacts[a], c.Plugins[i].Releases[j].Artifacts[b]
				if l.OS != r.OS {
					return l.OS < r.OS
				}
				return l.Arch < r.Arch
			})
		}
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	if len(b) > MaxCatalogBytes {
		return nil, errors.New("generated catalog exceeds Core runtime limit")
	}
	return b, nil
}

func BuildV2Projection(plugins []PluginV3) ([]byte, error) {
	return Build(V2Projection(plugins))
}

func isFirstPartyRepository(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) == 2 && strings.EqualFold(parts[0], "integrated-recorder") && parts[1] != ""
}

func cloneChannels(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneReleases(in []Release) []Release {
	if in == nil {
		return nil
	}
	out := append([]Release(nil), in...)
	for i := range out {
		out[i].Artifacts = append([]Artifact(nil), in[i].Artifacts...)
	}
	return out
}

func clonePluginsV3(in []PluginV3) []PluginV3 {
	out := make([]PluginV3, len(in))
	copy(out, in)
	for i := range out {
		out[i].Channels = cloneChannels(in[i].Channels)
		out[i].Releases = cloneReleases(in[i].Releases)
	}
	return out
}
