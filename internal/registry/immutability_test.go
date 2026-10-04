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
