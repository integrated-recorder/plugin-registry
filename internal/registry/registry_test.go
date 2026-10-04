package registry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func artifact(id, typ, osname, arch string) Artifact {
	prefix := "integrated-recorder-adapter-"
	if typ == "storage" {
		prefix = "integrated-recorder-storage-"
	}
	return Artifact{OS: osname, Arch: arch, URL: "https://downloads.example.org/releases/" + id + "/" + osname + "-" + arch, Filename: prefix + id, Size: 100, SHA256: strings.Repeat("a", 64)}
}
func plugin(id, typ, version string) Plugin {
	protocol := typ
	return Plugin{ID: id, Type: typ, Name: "Fixture " + id, Repository: "https://github.com/example/" + id, Channels: map[string]string{"stable": version}, Releases: []Release{{Version: version, Protocol: Protocol{Name: protocol, Version: 1}, SourceCommit: strings.Repeat("1", 40), Artifacts: []Artifact{artifact(id, typ, "linux", "amd64")}}}}
}
func sourceBytes(t *testing.T, p Plugin) []byte {
	t.Helper()
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestEmptyCatalogIsValid(t *testing.T) {
	c, e := DecodeCatalog([]byte(`{"schema_version":2,"plugins":[]}`))
	if e != nil || len(c.Plugins) != 0 {
		t.Fatalf("empty catalog: %#v %v", c, e)
	}
	b, e := Build(nil)
	if e != nil {
		t.Fatal(e)
	}
	if string(b) != "{\n  \"schema_version\": 2,\n  \"plugins\": []\n}\n" {
		t.Fatalf("unexpected empty output: %s", b)
	}
}

func TestValidSourceAndStorageAndChannelSubset(t *testing.T) {
	for _, p := range []Plugin{plugin("soop", "source", "0.1.0"), plugin("s3", "storage", "1.0.0")} {
		if _, e := ValidateSource(sourceBytes(t, p)); e != nil {
			t.Fatalf("%s rejected: %v", p.Type, e)
		}
	}
}

func TestStrictJSONRejectsMalformedShape(t *testing.T) {
	p := plugin("soop", "source", "0.1.0")
	base := sourceBytes(t, p)
	tests := map[string][]byte{
		"unknown":              bytes.Replace(base, []byte(`"id"`), []byte(`"extra":true,"id"`), 1),
		"case variant":         bytes.Replace(base, []byte(`"id"`), []byte(`"ID"`), 1),
		"duplicate nested key": []byte(`{"id":"soop","type":"source","name":"SOOP","repository":"https://github.com/e/soop","channels":{"stable":"0.1.0","stable":"0.1.0"},"releases":[]}`),
		"trailing":             append(append([]byte(nil), base...), []byte(` {}`)...),
		"null channel":         bytes.Replace(base, []byte(`"channels":{"stable":"0.1.0"}`), []byte(`"channels":null`), 1),
		"null array":           bytes.Replace(base, []byte(`"artifacts":[`), []byte(`"artifacts":null`), 1),
		"wrong root":           []byte(`null`),
	}
	badUTF8 := append([]byte(nil), base...)
	badUTF8[len(badUTF8)-1] = 0xff
	tests["invalid utf8"] = badUTF8
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, e := ValidateSource(data); e == nil {
				t.Fatal("expected strict rejection")
			}
		})
	}
}

func TestSemanticValidationRejectsInvalidEntries(t *testing.T) {
	cases := map[string]func(*Plugin){
		"bad id":                  func(p *Plugin) { p.ID = "Bad" },
		"wrong protocol family":   func(p *Plugin) { p.Releases[0].Protocol.Name = "storage" },
		"channel missing release": func(p *Plugin) { p.Channels["stable"] = "9.9" },
		"bad repository URL":      func(p *Plugin) { p.Repository = "http://github.com/example/soop" },
		"query URL":               func(p *Plugin) { p.Releases[0].Artifacts[0].URL += "?token=secret" },
		"userinfo URL":            func(p *Plugin) { p.Releases[0].Artifacts[0].URL = "https://user:pw@downloads.example.org/a" },
		"bad filename":            func(p *Plugin) { p.Releases[0].Artifacts[0].Filename = "other" },
		"bad size":                func(p *Plugin) { p.Releases[0].Artifacts[0].Size = MaxArtifactBytes + 1 },
		"bad digest":              func(p *Plugin) { p.Releases[0].Artifacts[0].SHA256 = strings.Repeat("A", 64) },
		"short commit":            func(p *Plugin) { p.Releases[0].SourceCommit = "abc1234" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := plugin("soop", "source", "0.1.0")
			mutate(&p)
			if err := Validate(Catalog{2, []Plugin{p}}); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
	if err := Validate(Catalog{2, []Plugin{plugin("local", "storage", "1.0.0")}}); err == nil {
		t.Fatal("remote storage.local must be rejected")
	}
	if err := Validate(Catalog{2, []Plugin{plugin("x", "source", "1"), plugin("x", "source", "2")}}); err == nil {
		t.Fatal("duplicate plugin ID accepted")
	}
	first := plugin("soop", "source", "1")
	second := plugin("twitch", "source", "1")
	second.Releases[0].Artifacts[0].Filename = first.Releases[0].Artifacts[0].Filename
	if err := Validate(Catalog{2, []Plugin{first, second}}); err == nil {
		t.Fatal("filename owned by another plugin accepted")
	}
	p := plugin("soop", "source", "1")
	p.Releases = append(p.Releases, p.Releases[0])
	if err := Validate(Catalog{2, []Plugin{p}}); err == nil {
		t.Fatal("duplicate release accepted")
	}
	p = plugin("soop", "source", "1")
	p.Releases[0].Artifacts = append(p.Releases[0].Artifacts, p.Releases[0].Artifacts[0])
	if err := Validate(Catalog{2, []Plugin{p}}); err == nil {
		t.Fatal("duplicate platform accepted")
	}
	if err := Validate(Catalog{2, []Plugin{plugin("same", "source", "1"), plugin("same", "source", "2")}}); err == nil {
		t.Fatal("duplicate filename across plugin ownership accepted")
	}
}

func TestSamePluginFilenameMayRepeatAcrossVersions(t *testing.T) {
	p := plugin("soop", "source", "1")
	second := p.Releases[0]
	second.Version = "2"
	second.SourceCommit = strings.Repeat("2", 40)
	p.Releases = append(p.Releases, second)
	p.Channels["stable"] = "2"
	if err := Validate(Catalog{2, []Plugin{p}}); err != nil {
		t.Fatalf("normal versioned executable filename rejected: %v", err)
	}
}

func TestBuildDeterministicSorting(t *testing.T) {
	a := plugin("z", "source", "2")
	a2 := a.Releases[0]
	a2.Version = "1"
	a2.SourceCommit = strings.Repeat("2", 40)
	a.Releases = append(a.Releases, a2)
	a.Releases[0].Artifacts = append(a.Releases[0].Artifacts, artifact("z", "source", "darwin", "arm64"))
	b := plugin("a", "storage", "1")
	first, e := Build([]Plugin{a, b})
	if e != nil {
		t.Fatal(e)
	}
	second, e := Build([]Plugin{b, a})
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("output differs for same tree")
	}
	c, e := DecodeCatalog(first)
	if e != nil {
		t.Fatal(e)
	}
	if c.Plugins[0].ID != "a" || c.Plugins[1].Releases[0].Version != "1" {
		t.Fatal("catalog order is not deterministic")
	}
	if c.Plugins[1].Releases[1].Artifacts[0].OS != "darwin" {
		t.Fatal("artifacts not sorted by OS")
	}
}

func TestLoadPluginsRequiresOneCanonicalObjectPerNamedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "source.soop.json"), sourceBytes(t, plugin("soop", "source", "1")), 0600); err != nil {
		t.Fatal(err)
	}
	got, e := LoadPlugins(dir)
	if e != nil || len(got) != 1 {
		t.Fatalf("load: %v %v", got, e)
	}
	if err := os.Symlink(filepath.Join(dir, "source.soop.json"), filepath.Join(dir, "source.other.json")); err != nil {
		t.Skip(err)
	}
	if _, e := LoadPlugins(dir); e == nil {
		t.Fatal("symlink source accepted")
	}
}

func TestCheckedInExamplesAreClearlyNonProductionFixtures(t *testing.T) {
	for _, name := range []string{"source.fixture.json", "storage.fixture.json"} {
		b, e := os.ReadFile(filepath.Join("..", "..", "testdata", "valid", name))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = ValidateSource(b); e != nil {
			t.Fatalf("fixture %s is invalid: %v", name, e)
		}
	}
	bad, e := os.ReadFile(filepath.Join("..", "..", "testdata", "invalid", "wrong-protocol.json"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ValidateSource(bad); e == nil {
		t.Fatal("invalid fixture accepted")
	}
}
