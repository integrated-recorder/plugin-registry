// Package registry implements the curated, static Plugin Registry v2 source format.
package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	SchemaVersion                = 2
	MaxCatalogBytes              = 2 << 20
	MaxPlugins                   = 256
	MaxReleasesPerPlugin         = 128
	MaxArtifactsPerRelease       = 16
	MaxArtifactBytes       int64 = 536870912
	PinnedCoreCommit             = "3cf2c90280873f98f5120f67e39516c5af8abc04"
)

var (
	pluginIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	identityRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]{0,127}$`)
	commitRE   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	shaRE      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Catalog struct {
	SchemaVersion int      `json:"schema_version"`
	Plugins       []Plugin `json:"plugins"`
}
type Plugin struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Repository string            `json:"repository"`
	Channels   map[string]string `json:"channels"`
	Releases   []Release         `json:"releases"`
}
type Release struct {
	Version      string     `json:"version"`
	Protocol     Protocol   `json:"protocol"`
	SourceCommit string     `json:"source_commit"`
	Artifacts    []Artifact `json:"artifacts"`
}
type Protocol struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}
type Artifact struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

func LoadPlugins(dir string) ([]Plugin, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read plugins directory: %w", err)
	}
	plugins := make([]Plugin, 0)
	for _, ent := range entries {
		name := ent.Name()
		if name == ".gitkeep" {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			return nil, fmt.Errorf("unexpected file in plugins directory: %s", safeName(name))
		}
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("inspect plugin file: %w", err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > MaxCatalogBytes {
			return nil, fmt.Errorf("plugin source file is not a bounded regular file: %s", safeName(name))
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read plugin source: %w", err)
		}
		var p Plugin
		if err := decodeExact(data, sourceShapes, &p); err != nil {
			return nil, fmt.Errorf("%s: %w", safeName(name), err)
		}
		wantFile := p.Type + "." + p.ID + ".json"
		if name != wantFile {
			return nil, fmt.Errorf("plugin source filename must be %s", wantFile)
		}
		plugins = append(plugins, p)
	}
	c := Catalog{SchemaVersion: SchemaVersion, Plugins: plugins}
	if err := Validate(c); err != nil {
		return nil, err
	}
	return c.Plugins, nil
}

// decodeExact detects duplicate keys, invalid UTF-8, null/missing/unknown fields,
// case variants, and trailing JSON before decoding into a typed value.
func decodeExact(data []byte, shapes map[string][]string, dst any) error {
	if len(data) == 0 || len(data) > MaxCatalogBytes || !utf8.Valid(data) {
		return errors.New("invalid or oversized UTF-8 JSON")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	value, err := parseValue(d)
	if err != nil {
		return errors.New("invalid JSON or duplicate object key")
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	if err := checkShapes(value, "", shapes); err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return errors.New("invalid JSON value")
	}
	d2 := json.NewDecoder(bytes.NewReader(b))
	d2.DisallowUnknownFields()
	if err := d2.Decode(dst); err != nil {
		return errors.New("invalid registry object shape")
	}
	return nil
}

func parseValue(d *json.Decoder) (any, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := t.(json.Delim); ok {
		switch delim {
		case '{':
			m := map[string]any{}
			for d.More() {
				kt, e := d.Token()
				if e != nil {
					return nil, e
				}
				k, ok := kt.(string)
				if !ok {
					return nil, errors.New("object key")
				}
				if _, exists := m[k]; exists {
					return nil, errors.New("duplicate key")
				}
				v, e := parseValue(d)
				if e != nil {
					return nil, e
				}
				m[k] = v
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return nil, errors.New("object end")
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				v, e := parseValue(d)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return nil, errors.New("array end")
			}
			return a, nil
		default:
			return nil, errors.New("unexpected delimiter")
		}
	}
	return t, nil
}

func checkShapes(value any, path string, shapes map[string][]string) error {
	if m, ok := value.(map[string]any); ok {
		allowed, known := shapes[path]
		if !known {
			return fmt.Errorf("unexpected object at %s", path)
		}
		set := map[string]bool{}
		for _, k := range allowed {
			set[k] = true
		}
		for k, v := range m {
			if !set[k] {
				return fmt.Errorf("unknown field %q at %s", k, path)
			}
			if v == nil {
				return fmt.Errorf("null field %q at %s", k, path)
			}
			if err := checkShapes(v, childPath(path, k), shapes); err != nil {
				return err
			}
		}
		optionalMap := strings.HasSuffix(path, "channels")
		if !optionalMap {
			for _, k := range allowed {
				if _, ok := m[k]; !ok {
					return fmt.Errorf("missing field %q at %s", k, path)
				}
			}
		}
	} else if a, ok := value.([]any); ok {
		for _, v := range a {
			if v == nil {
				return fmt.Errorf("null array item at %s", path)
			}
			if err := checkShapes(v, path+"[]", shapes); err != nil {
				return err
			}
		}
	}
	return nil
}
func childPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

var sourceShapes = map[string][]string{
	"":                       {"id", "type", "name", "repository", "channels", "releases"},
	"channels":               {"stable", "beta", "development"},
	"releases[]":             {"version", "protocol", "source_commit", "artifacts"},
	"releases[].protocol":    {"name", "version"},
	"releases[].artifacts[]": {"os", "arch", "url", "filename", "size", "sha256"},
}
var catalogShapes = map[string][]string{
	"":                                 {"schema_version", "plugins"},
	"plugins[]":                        {"id", "type", "name", "repository", "channels", "releases"},
	"plugins[].channels":               {"stable", "beta", "development"},
	"plugins[].releases[]":             {"version", "protocol", "source_commit", "artifacts"},
	"plugins[].releases[].protocol":    {"name", "version"},
	"plugins[].releases[].artifacts[]": {"os", "arch", "url", "filename", "size", "sha256"},
}

func ValidateSource(data []byte) (Plugin, error) {
	var p Plugin
	if err := decodeExact(data, sourceShapes, &p); err != nil {
		return Plugin{}, err
	}
	if err := Validate(Catalog{SchemaVersion: SchemaVersion, Plugins: []Plugin{p}}); err != nil {
		return Plugin{}, err
	}
	return p, nil
}
func DecodeCatalog(data []byte) (Catalog, error) {
	var c Catalog
	if err := decodeExact(data, catalogShapes, &c); err != nil {
		return Catalog{}, err
	}
	if err := Validate(c); err != nil {
		return Catalog{}, err
	}
	return c, nil
}

func Validate(c Catalog) error {
	if c.SchemaVersion != SchemaVersion {
		return errors.New("catalog schema_version must be 2")
	}
	if len(c.Plugins) > MaxPlugins {
		return errors.New("catalog exceeds plugin limit")
	}
	ids := map[string]bool{}
	filenames := map[string]string{}
	for _, p := range c.Plugins {
		if !pluginIDRE.MatchString(p.ID) {
			return fmt.Errorf("invalid plugin ID %q", p.ID)
		}
		if ids[p.ID] {
			return fmt.Errorf("duplicate plugin ID %q", p.ID)
		}
		ids[p.ID] = true
		if p.Type != "source" && p.Type != "storage" {
			return fmt.Errorf("plugin %s has invalid type", p.ID)
		}
		if p.Type == "storage" && p.ID == "local" {
			return errors.New("remote storage plugin ID local is reserved for bundled storage.local")
		}
		if !validText(p.Name, 128) {
			return fmt.Errorf("plugin %s has invalid name", p.ID)
		}
		if len(p.Repository) > 512 {
			return fmt.Errorf("plugin %s repository URL exceeds Core bounds", p.ID)
		}
		if !validURL(p.Repository, false) {
			return fmt.Errorf("plugin %s repository must be a public HTTPS URL without query, fragment, or userinfo", p.ID)
		}
		if len(p.Releases) == 0 || len(p.Releases) > MaxReleasesPerPlugin {
			return fmt.Errorf("plugin %s release count is invalid", p.ID)
		}
		if len(p.Channels) == 0 || len(p.Channels) > 3 {
			return fmt.Errorf("plugin %s channel count is invalid", p.ID)
		}
		versions := map[string]bool{}
		for _, r := range p.Releases {
			if !identityRE.MatchString(r.Version) {
				return fmt.Errorf("plugin %s has invalid version", p.ID)
			}
			if versions[r.Version] {
				return fmt.Errorf("plugin %s has duplicate version %q", p.ID, r.Version)
			}
			versions[r.Version] = true
			if r.Protocol.Name != p.Type || r.Protocol.Version != 1 {
				return fmt.Errorf("plugin %s release %s protocol does not match plugin type", p.ID, r.Version)
			}
			if !commitRE.MatchString(r.SourceCommit) {
				return fmt.Errorf("plugin %s release %s source_commit must be a full lowercase 40-hex commit", p.ID, r.Version)
			}
			if len(r.Artifacts) == 0 || len(r.Artifacts) > MaxArtifactsPerRelease {
				return fmt.Errorf("plugin %s release %s artifact count is invalid", p.ID, r.Version)
			}
			platforms := map[string]bool{}
			for _, a := range r.Artifacts {
				if (a.OS != "linux" && a.OS != "darwin") || (a.Arch != "amd64" && a.Arch != "arm64") {
					return fmt.Errorf("plugin %s release %s has unsupported platform", p.ID, r.Version)
				}
				key := a.OS + "/" + a.Arch
				if platforms[key] {
					return fmt.Errorf("plugin %s release %s duplicates platform %s", p.ID, r.Version, key)
				}
				platforms[key] = true
				if !validURL(a.URL, true) {
					return fmt.Errorf("plugin %s release %s artifact URL must be public HTTPS without query, fragment, or userinfo", p.ID, r.Version)
				}
				prefix := "integrated-recorder-adapter-"
				if p.Type == "storage" {
					prefix = "integrated-recorder-storage-"
				}
				if a.Filename != prefix+p.ID || len(a.Filename) > 92 {
					return fmt.Errorf("plugin %s release %s filename must be %s", p.ID, r.Version, prefix+p.ID)
				}
				if owner, exists := filenames[a.Filename]; exists && owner != p.ID {
					return fmt.Errorf("executable filename %q is reused by plugins %s and %s", a.Filename, owner, p.ID)
				}
				filenames[a.Filename] = p.ID
				if a.Size <= 0 || a.Size > MaxArtifactBytes {
					return fmt.Errorf("plugin %s release %s artifact size is outside bounds", p.ID, r.Version)
				}
				if !shaRE.MatchString(a.SHA256) {
					return fmt.Errorf("plugin %s release %s SHA-256 must be lowercase 64-hex", p.ID, r.Version)
				}
			}
		}
		for channel, version := range p.Channels {
			if channel != "stable" && channel != "beta" && channel != "development" {
				return fmt.Errorf("plugin %s has invalid channel", p.ID)
			}
			if !versions[version] {
				return fmt.Errorf("plugin %s channel %s points to missing release", p.ID, channel)
			}
		}
	}
	return nil
}

func validText(s string, max int) bool {
	if len(s) == 0 || len(s) > max || !utf8.ValidString(s) || strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if r == 0 || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validURL(raw string, repository bool) bool {
	if len(raw) == 0 || len(raw) > 2048 || !utf8.ValidString(raw) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || u.Opaque != "" || strings.ContainsAny(u.Host, "\r\n\\") {
		return false
	}
	if repository && (u.Path == "" || u.Path == "/") {
		return false
	}
	if !repository && u.Path == "" {
		return false
	}
	if p := u.Port(); p != "" {
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
func safeName(s string) string {
	if len(s) > 128 {
		return s[:128]
	}
	return filepath.Base(s)
}

func Build(plugins []Plugin) ([]byte, error) {
	items := append([]Plugin{}, plugins...)
	c := Catalog{SchemaVersion: SchemaVersion, Plugins: items}
	if err := Validate(c); err != nil {
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

func DecodePluginObject(data []byte) (Plugin, error) { return ValidateSource(data) }
func EqualJSON(a, b []byte) bool                     { return bytes.Equal(a, b) }

var nonPublicNetworks = func() []*net.IPNet {
	blocks := []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/128", "::1/128", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "fc00::/7", "fe80::/10", "ff00::/8"}
	out := make([]*net.IPNet, 0, len(blocks))
	for _, block := range blocks {
		_, network, err := net.ParseCIDR(block)
		if err == nil {
			out = append(out, network)
		}
	}
	return out
}()

func IsPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	isV4 := ip.To4() != nil
	if isV4 {
		ip = ip.To4()
	}
	for _, network := range nonPublicNetworks {
		if isV4 != (network.IP.To4() != nil) {
			continue
		}
		if network.Contains(ip) {
			return false
		}
	}
	return true
}
