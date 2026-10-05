package registry

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Registry Test", "GIT_AUTHOR_EMAIL=registry-test@example.invalid", "GIT_COMMITTER_NAME=Registry Test", "GIT_COMMITTER_EMAIL=registry-test@example.invalid")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}
func fixtureRepo(t *testing.T) (string, Plugin) {
	t.Helper()
	dir := t.TempDir()
	gitTest(t, dir, "init", "-b", "main")
	_ = os.Mkdir(filepath.Join(dir, "plugins"), 0755)
	p := plugin("soop", "source", "1.0.0")
	if err := os.WriteFile(filepath.Join(dir, "plugins", "source.soop.json"), sourceBytes(t, p), 0644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "plugins")
	gitTest(t, dir, "commit", "-m", "base")
	return dir, p
}
func writePlugin(t *testing.T, dir string, p Plugin) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "plugins", "source.soop.json"), sourceBytes(t, p), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckImmutabilityAllowsNewReleaseAndChannelMove(t *testing.T) {
	dir, p := fixtureRepo(t)
	newRelease := p.Releases[0]
	newRelease.Version = "2.0.0"
	newRelease.SourceCommit = strings.Repeat("2", 40)
	newRelease.Artifacts = append([]Artifact(nil), newRelease.Artifacts...)
	newRelease.Artifacts[0].SHA256 = strings.Repeat("b", 64)
	p.Releases = append(p.Releases, newRelease)
	p.Channels["stable"] = "2.0.0"
	writePlugin(t, dir, p)
	if err := CheckImmutability(dir, "main", filepath.Join(dir, "plugins")); err != nil {
		t.Fatalf("allowed append/channel move rejected: %v", err)
	}
}

func TestCheckImmutabilityRejectsChangedOrDeletedRelease(t *testing.T) {
	for _, mode := range []string{"changed digest", "deleted release"} {
		t.Run(mode, func(t *testing.T) {
			dir, p := fixtureRepo(t)
			if mode == "changed digest" {
				p.Releases[0].Artifacts[0].SHA256 = strings.Repeat("b", 64)
			} else {
				p.Releases = nil
			}
			writePlugin(t, dir, p)
			if err := CheckImmutability(dir, "main", filepath.Join(dir, "plugins")); err == nil {
				t.Fatal("immutable release change accepted")
			}
		})
	}
}

func TestCheckImmutabilityRejectsPluginDeletion(t *testing.T) {
	dir, _ := fixtureRepo(t)
	if err := os.Remove(filepath.Join(dir, "plugins", "source.soop.json")); err != nil {
		t.Fatal(err)
	}
	if err := CheckImmutability(dir, "main", filepath.Join(dir, "plugins")); err == nil {
		t.Fatal("plugin/release deletion accepted")
	}
}

func fixtureRepoV3(t *testing.T) (string, PluginV3) {
	t.Helper()
	dir := t.TempDir()
	gitTest(t, dir, "init", "-b", "main")
	pluginsDir := filepath.Join(dir, "plugins")
	_ = os.Mkdir(pluginsDir, 0755)
	p := pluginV3("soop", "source", "1.0.0", "third_party", "https://github.com/example/soop")
	if err := os.WriteFile(filepath.Join(pluginsDir, "source.soop.json"), sourceBytesV3(t, p), 0644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", "plugins")
	gitTest(t, dir, "commit", "-m", "base")
	return dir, p
}

func writePluginV3(t *testing.T, dir string, p PluginV3) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "plugins", "source.soop.json"), sourceBytesV3(t, p), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckImmutabilityV3AllowsAppendAndChannelMove(t *testing.T) {
	dir, p := fixtureRepoV3(t)
	next := p.Releases[0]
	next.Version = "2.0.0"
	next.SourceCommit = strings.Repeat("2", 40)
	next.Artifacts = append([]Artifact(nil), next.Artifacts...)
	next.Artifacts[0].SHA256 = strings.Repeat("b", 64)
	p.Releases = append(p.Releases, next)
	p.Channels["stable"] = "2.0.0"
	writePluginV3(t, dir, p)
	if err := CheckImmutabilityV3(dir, "main", filepath.Join(dir, "plugins")); err != nil {
		t.Fatalf("channel movement and new release should be allowed: %v", err)
	}
}

func TestCheckImmutabilityV3RejectsReleaseMutationAndDeletion(t *testing.T) {
	for _, mode := range []string{"changed digest", "deleted release"} {
		t.Run(mode, func(t *testing.T) {
			dir, p := fixtureRepoV3(t)
			if mode == "changed digest" {
				p.Releases[0].Artifacts[0].SHA256 = strings.Repeat("f", 64)
			} else {
				p.Releases = nil
			}
			writePluginV3(t, dir, p)
			if err := CheckImmutabilityV3(dir, "main", filepath.Join(dir, "plugins")); err == nil {
				t.Fatal("v3 approved release mutation accepted")
			}
		})
	}
}

func TestCheckImmutabilityV3AcceptsV2BaseProjection(t *testing.T) {
	dir, p := fixtureRepo(t)
	v3 := pluginV3(p.ID, p.Type, p.Releases[0].Version, "third_party", p.Repository)
	writePluginV3(t, dir, v3)
	if err := CheckImmutabilityV3(dir, "main", filepath.Join(dir, "plugins")); err != nil {
		t.Fatalf("v2 history migration should preserve the exact release identity: %v", err)
	}
}
