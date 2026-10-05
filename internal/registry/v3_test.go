package registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pluginV3(id, typ, version, publisher, repository string) PluginV3 {
	p := plugin(id, typ, version)
	return PluginV3{
		ID: p.ID, Type: p.Type, Publisher: Publisher{Kind: publisher}, Name: p.Name,
		Repository: repository, Channels: p.Channels, Releases: p.Releases,
	}
}

func sourceBytesV3(t *testing.T, p PluginV3) []byte {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestV3ValidPublishersAndEmptyCatalog(t *testing.T) {
	first := pluginV3("source.soop", "source", "0.1.0", "first_party", "https://github.com/integrated-recorder/source.soop")
	third := pluginV3("storage-s3", "storage", "1.0.0", "third_party", "https://github.com/example/storage-s3")
	for _, p := range []PluginV3{first, third} {
		if _, err := ValidateSourceV3(sourceBytesV3(t, p)); err != nil {
			t.Fatalf("valid %s plugin rejected: %v", p.Publisher.Kind, err)
		}
	}
	if _, err := ValidateSourceV3(sourceBytesV3(t, pluginV3("third", "source", "1", "third_party", "https://code.example/owner/source"))); err != nil {
		t.Fatalf("third-party non-GitHub repository rejected: %v", err)
	}
	data, err := BuildV3(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\n  \"schema_version\": 3,\n  \"plugins\": []\n}\n" {
		t.Fatalf("unexpected empty v3 output: %s", data)
	}
	if _, err := DecodeCatalogV3(data); err != nil {
		t.Fatalf("empty v3 catalog failed decode: %v", err)
	}
	compat, err := BuildV2Projection(nil)
	if err != nil || string(compat) != "{\n  \"schema_version\": 2,\n  \"plugins\": []\n}\n" {
		t.Fatalf("unexpected empty v2 compatibility output: %s (%v)", compat, err)
	}
}

func TestVendoredV3SchemaContentHash(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "plugin-registry-v3.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	const want = "b282ceeda7911112f6634b2f0f2ab9c17901f5c571ea02f91752dc894708f68e"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("vendored Core v3 schema drift: got %s want %s", got, want)
	}
}

func TestV3RejectsStrictShapeAndPublisherPolicyViolations(t *testing.T) {
	valid := pluginV3("soop", "source", "1.0.0", "third_party", "https://github.com/example/soop")
	base := sourceBytesV3(t, valid)
	tests := map[string][]byte{
		"unknown publisher field": bytes.Replace(base, []byte(`"kind"`), []byte(`"unexpected":"x","kind"`), 1),
		"wrong case":              bytes.Replace(base, []byte(`"publisher"`), []byte(`"Publisher"`), 1),
		"null publisher":          bytes.Replace(base, []byte(`"publisher":{"kind":"third_party"}`), []byte(`"publisher":null`), 1),
		"missing publisher":       bytes.Replace(base, []byte(`"publisher":{"kind":"third_party"},`), nil, 1),
		"null publisher kind":     bytes.Replace(base, []byte(`"kind":"third_party"`), []byte(`"kind":null`), 1),
		"duplicate publisher key": []byte(`{"id":"soop","type":"source","publisher":{"kind":"third_party","kind":"third_party"},"name":"SOOP","repository":"https://github.com/example/soop","channels":{"stable":"1.0.0"},"releases":[]}`),
		"trailing JSON":           append(append([]byte(nil), base...), []byte(` {}`)...),
	}
	invalidUTF8 := append([]byte(nil), base...)
	invalidUTF8[len(invalidUTF8)-1] = 0xff
	tests["invalid UTF-8"] = invalidUTF8
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateSourceV3(data); err == nil {
				t.Fatal("invalid v3 source accepted")
			}
		})
	}

	cases := map[string]PluginV3{
		"publisher kind": func() PluginV3 { p := valid; p.Publisher.Kind = "community"; return p }(),
		"first party external owner": func() PluginV3 {
			return pluginV3("soop", "source", "1", "first_party", "https://github.com/another-org/soop")
		}(),
		"first party non GitHub": func() PluginV3 {
			return pluginV3("soop", "source", "1", "first_party", "https://git.example/integrated-recorder/soop")
		}(),
		"first party nonstandard port": func() PluginV3 {
			return pluginV3("soop", "source", "1", "first_party", "https://github.com:444/integrated-recorder/soop")
		}(),
		"reserved source hls":    pluginV3("hls", "source", "1", "third_party", "https://github.com/example/hls"),
		"reserved storage local": pluginV3("local", "storage", "1", "first_party", "https://github.com/integrated-recorder/storage.local"),
		"protocol family": func() PluginV3 {
			p := valid
			p.Releases[0].Protocol.Name = "storage"
			return p
		}(),
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateV3(CatalogV3{SchemaVersion: 3, Plugins: []PluginV3{p}}); err == nil {
				t.Fatal("policy-invalid v3 plugin accepted")
			}
		})
	}
	if err := ValidateV3(CatalogV3{SchemaVersion: 3, Plugins: []PluginV3{valid, valid}}); err == nil {
		t.Fatal("duplicate v3 plugin accepted")
	}
}

func TestV3DeterministicBuilderAndV2Projection(t *testing.T) {
	a := pluginV3("zeta", "source", "2", "third_party", "https://github.com/example/zeta")
	a2 := a.Releases[0]
	a2.Version = "1"
	a2.SourceCommit = strings.Repeat("2", 40)
	a.Releases = append(a.Releases, a2)
	a.Releases[0].Artifacts = append(a.Releases[0].Artifacts, artifact("zeta", "source", "darwin", "arm64"))
	b := pluginV3("alpha", "storage", "1", "first_party", "https://github.com/integrated-recorder/storage.alpha")
	first, err := BuildV3([]PluginV3{a, b})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildV3([]PluginV3{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("v3 output differs for equivalent source trees")
	}
	c, err := DecodeCatalogV3(first)
	if err != nil {
		t.Fatal(err)
	}
	if c.Plugins[0].ID != "alpha" || c.Plugins[1].Releases[0].Version != "1" || c.Plugins[1].Releases[1].Artifacts[0].OS != "darwin" {
		t.Fatalf("v3 output order is unstable: %#v", c)
	}
	compat, err := BuildV2Projection([]PluginV3{a, b})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCatalog(compat)
	if err != nil || len(decoded.Plugins) != 2 {
		t.Fatalf("v2 compatibility catalog invalid: %v", err)
	}
	if strings.Contains(string(compat), "publisher") {
		t.Fatal("publisher-only v3 field leaked into v2 projection")
	}
}

func TestLoadPluginsV3RequiresCanonicalOnePluginFiles(t *testing.T) {
	dir := t.TempDir()
	p := pluginV3("soop", "source", "1", "third_party", "https://github.com/example/soop")
	if err := os.WriteFile(filepath.Join(dir, "source.soop.json"), sourceBytesV3(t, p), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPluginsV3(dir)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("load v3 plugin: %v %v", loaded, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.other.json"), sourceBytesV3(t, p), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPluginsV3(dir); err == nil {
		t.Fatal("filename mismatch accepted")
	}
}

func TestCheckedInV3FixturesAreNonProductionOnly(t *testing.T) {
	valid, err := os.ReadFile(filepath.Join("..", "..", "testdata", "valid", "source.third-party-v3.fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateSourceV3(valid); err != nil {
		t.Fatalf("valid v3 fixture rejected: %v", err)
	}
	invalid, err := os.ReadFile(filepath.Join("..", "..", "testdata", "invalid", "first-party-outside-org-v3.fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateSourceV3(invalid); err == nil {
		t.Fatal("invalid v3 first-party fixture accepted")
	}
}

func TestBuildPagesV3PublishesCompatibilityAndCanonicalCatalogs(t *testing.T) {
	schemas := t.TempDir()
	for _, name := range []string{"plugin-registry-v1.schema.json", "plugin-registry-v2.schema.json", "plugin-registry-v3.schema.json"} {
		if err := os.WriteFile(filepath.Join(schemas, name), []byte("{}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	out := t.TempDir()
	if err := BuildPagesV3(nil, schemas, out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"catalog.json", "catalog-v3.json", "index.html", "plugin-registry-v1.schema.json", "plugin-registry-v2.schema.json", "plugin-registry-v3.schema.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("expected Pages output %s: %v", name, err)
		}
	}
	v2, err := os.ReadFile(filepath.Join(out, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCatalog(v2); err != nil {
		t.Fatalf("Pages v2 compatibility catalog invalid: %v", err)
	}
	v3, err := os.ReadFile(filepath.Join(out, "catalog-v3.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCatalogV3(v3); err != nil {
		t.Fatalf("Pages v3 catalog invalid: %v", err)
	}
	index, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil || !strings.Contains(string(index), "catalog-v3.json") || !strings.Contains(string(index), "catalog.json") {
		t.Fatalf("Pages index does not link both catalogs: %v", err)
	}
}
