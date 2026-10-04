package registry

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type releaseIdentity struct {
	pluginType      string
	protocolName    string
	protocolVersion int
	sourceCommit    string
	url             string
	filename        string
	size            int64
	sha256          string
}

// CheckImmutability compares the current source tree with a Git base ref. A
// merged release may gain platforms only as a new version; existing release
// platform identities and their approved bytes are append-only.
func CheckImmutability(repoDir, baseRef, pluginsDir string) error {
	if strings.TrimSpace(baseRef) == "" {
		return fmt.Errorf("base Git ref is required")
	}
	if !filepath.IsAbs(pluginsDir) {
		pluginsDir = filepath.Join(repoDir, pluginsDir)
	}
	current, err := LoadPlugins(pluginsDir)
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
		p, e := ValidateSource(data)
		if e != nil {
			return fmt.Errorf("base plugin source is invalid: %w", e)
		}
		base[p.ID] = p
	}
	now := map[string]Plugin{}
	for _, p := range current {
		now[p.ID] = p
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

func releaseIdentities(p Plugin) map[string]releaseIdentity {
	out := map[string]releaseIdentity{}
	for _, r := range p.Releases {
		for _, a := range r.Artifacts {
			key := p.ID + "@" + r.Version + "#" + a.OS + "/" + a.Arch
			out[key] = releaseIdentity{p.Type, r.Protocol.Name, r.Protocol.Version, r.SourceCommit, a.URL, a.Filename, a.Size, a.SHA256}
		}
	}
	return out
}

func gitOutput(repoDir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s failed", strings.Join(args[:min(2, len(args))], " "))
	}
	return bytes.TrimRight(out, "\n"), nil
}
