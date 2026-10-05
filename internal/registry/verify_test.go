package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func tlsServer(handler http.Handler) *httptest.Server {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.StartTLS()
	return server
}

func testOptions(t *testing.T, s *httptest.Server) verifyOptions {
	t.Helper()
	return verifyOptions{Client: s.Client(), Lookup: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("127.0.0.1")}, nil }, AllowPrivateForTest: true, TempDir: t.TempDir()}
}
func testArtifact(s *httptest.Server, b []byte) Artifact {
	sum := sha256.Sum256(b)
	return Artifact{OS: "linux", Arch: "amd64", URL: s.URL + "/artifact", Filename: "integrated-recorder-adapter-test", Size: int64(len(b)), SHA256: hex.EncodeToString(sum[:])}
}
func TestArtifactDownloadVerificationCases(t *testing.T) {
	payload := []byte("binary fixture body")
	t.Run("success", func(t *testing.T) {
		s := tlsServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
		defer s.Close()
		a := testArtifact(s, payload)
		opts := testOptions(t, s)
		file, e := downloadArtifact(context.Background(), opts, a)
		if e != nil {
			t.Fatal(e)
		}
		removeStagedArtifact(file)
	})
	t.Run("404", func(t *testing.T) {
		s := tlsServer(http.NotFoundHandler())
		defer s.Close()
		a := testArtifact(s, payload)
		opts := testOptions(t, s)
		if _, e := downloadArtifact(context.Background(), opts, a); e == nil {
			t.Fatal("404 accepted")
		}
	})
	t.Run("redirect", func(t *testing.T) {
		var s *httptest.Server
		s = tlsServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/artifact" {
				http.Redirect(w, r, "/target", http.StatusFound)
				return
			}
			_, _ = w.Write(payload)
		}))
		defer s.Close()
		a := testArtifact(s, payload)
		opts := testOptions(t, s)
		file, e := downloadArtifact(context.Background(), opts, a)
		if e != nil {
			t.Fatal(e)
		}
		removeStagedArtifact(file)
	})
	t.Run("http redirect rejected", func(t *testing.T) {
		s := tlsServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://example.org/object", http.StatusFound)
		}))
		defer s.Close()
		a := testArtifact(s, payload)
		opts := testOptions(t, s)
		if _, e := downloadArtifact(context.Background(), opts, a); e == nil {
			t.Fatal("HTTP redirect accepted")
		}
	})
	t.Run("redirect loop bounded", func(t *testing.T) {
		s := tlsServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/again", http.StatusFound) }))
		defer s.Close()
		a := testArtifact(s, payload)
		opts := testOptions(t, s)
		if _, e := downloadArtifact(context.Background(), opts, a); e == nil {
			t.Fatal("redirect loop accepted")
		}
	})
	t.Run("oversized", func(t *testing.T) {
		s := tlsServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
		defer s.Close()
		a := testArtifact(s, payload)
		a.Size--
		opts := testOptions(t, s)
		if _, e := downloadArtifact(context.Background(), opts, a); e == nil {
			t.Fatal("oversized response accepted")
		}
	})
	t.Run("truncated", func(t *testing.T) {
		s := tlsServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)+10))
			_, _ = w.Write(payload)
		}))
		defer s.Close()
		a := testArtifact(s, payload)
		a.Size += 10
		opts := testOptions(t, s)
		if _, e := downloadArtifact(context.Background(), opts, a); e == nil {
			t.Fatal("truncated response accepted")
		}
	})
	t.Run("wrong expected size", func(t *testing.T) {
		s := tlsServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
		defer s.Close()
		a := testArtifact(s, payload)
		a.Size++
		opts := testOptions(t, s)
		if _, e := downloadArtifact(context.Background(), opts, a); e == nil {
			t.Fatal("size mismatch accepted")
		}
	})
	t.Run("wrong SHA", func(t *testing.T) {
		s := tlsServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
		defer s.Close()
		a := testArtifact(s, payload)
		a.SHA256 = strings.Repeat("f", 64)
		opts := testOptions(t, s)
		if _, e := downloadArtifact(context.Background(), opts, a); e == nil {
			t.Fatal("digest mismatch accepted")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		s := tlsServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer s.Close()
		a := testArtifact(s, payload)
		opts := testOptions(t, s)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, e := downloadArtifact(ctx, opts, a); e == nil {
			t.Fatal("cancelled download accepted")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		s := tlsServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer s.Close()
		a := testArtifact(s, payload)
		opts := testOptions(t, s)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if _, e := downloadArtifact(ctx, opts, a); e == nil {
			t.Fatal("timed out download accepted")
		}
	})
}

func TestPublicAddressPolicy(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1", "192.168.0.1", "100.64.1.2", "198.18.0.1", "2001:db8::1"} {
		if IsPublicIP(net.ParseIP(ip)) {
			t.Fatalf("private IP accepted: %s", ip)
		}
	}
	if !IsPublicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
}

func TestAdapterDescribeFrameUsesCoreEightMiBLimit(t *testing.T) {
	if maxAdapterFrameBytes != 8<<20 {
		t.Fatalf("Core Protocol v1 frame limit drifted: %d", maxAdapterFrameBytes)
	}
	for _, size := range []int{maxAdapterFrameBytes, maxAdapterFrameBytes + 1} {
		data := make([]byte, size+1)
		for i := 0; i < size; i++ {
			data[i] = 'x'
		}
		data[size] = '\n'
		frame, err := readBoundedProtocolFrame(bytes.NewReader(data))
		if size == maxAdapterFrameBytes && (err != nil || len(frame) != maxAdapterFrameBytes) {
			t.Fatalf("valid maximum frame rejected: %v", err)
		}
		if size > maxAdapterFrameBytes && err == nil {
			t.Fatal("oversized frame unexpectedly accepted")
		}
	}
}

func TestGitHubReleaseMustListEveryRegistryArtifact(t *testing.T) {
	artifacts := []Artifact{{Filename: "a", URL: "https://cdn.example/a", Size: 10}, {Filename: "b", URL: "https://cdn.example/b", Size: 20}}
	assets := []githubAsset{{Name: "a", URL: "https://cdn.example/a", Size: 10}}
	if err := verifyReleaseAssets(artifacts, assets); err == nil {
		t.Fatal("release with a missing platform asset passed")
	}
	assets = append(assets, githubAsset{Name: "b", URL: "https://cdn.example/b", Size: 20})
	if err := verifyReleaseAssets(artifacts, assets); err != nil {
		t.Fatalf("complete release rejected: %v", err)
	}
}

func TestGitHubReleaseAssetNamesMayDifferFromInstalledExecutableName(t *testing.T) {
	artifacts := []Artifact{
		{Filename: "integrated-recorder-adapter-soop", URL: "https://github.com/integrated-recorder/source.soop/releases/download/v1/soop-linux-amd64", Size: 10},
		{Filename: "integrated-recorder-adapter-soop", URL: "https://github.com/integrated-recorder/source.soop/releases/download/v1/soop-linux-arm64", Size: 20},
	}
	assets := []githubAsset{
		{Name: "soop-linux-amd64", URL: artifacts[0].URL, Size: 10},
		{Name: "soop-linux-arm64", URL: artifacts[1].URL, Size: 20},
	}
	if err := verifyReleaseAssets(artifacts, assets); err != nil {
		t.Fatalf("platform-specific GitHub asset names should be accepted: %v", err)
	}
}

func TestVerifySourceCommitUsesBoundedGitObjectEndpoint(t *testing.T) {
	const sha = "f81b75a4bd223d33855142d1c037d7108b1ef1ce"
	body := `{"sha":"` + sha + `","verification":{"payload":"` + strings.Repeat("x", 128<<10) + `"}}`
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		want := "/repos/integrated-recorder/source.owncast/git/commits/" + sha
		if r.URL.Path != want {
			t.Fatalf("unexpected GitHub endpoint %q, want %q", r.URL.Path, want)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}
	if err := verifySourceCommit(context.Background(), client, "https://github.com/integrated-recorder/source.owncast", sha); err != nil {
		t.Fatalf("large Git object response should verify by SHA: %v", err)
	}
}

func TestBuildPagesPublishesOnlyProductionFiles(t *testing.T) {
	plugins := []Plugin{plugin("source", "source", "1")}
	dir := t.TempDir()
	schemas := t.TempDir()
	for _, n := range []string{"plugin-registry-v1.schema.json", "plugin-registry-v2.schema.json"} {
		if e := os.WriteFile(filepath.Join(schemas, n), []byte("{}"), 0644); e != nil {
			t.Fatal(e)
		}
	}
	if e := BuildPages(plugins, schemas, dir); e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"catalog.json", "index.html", "plugin-registry-v1.schema.json", "plugin-registry-v2.schema.json"} {
		if _, e := os.Stat(filepath.Join(dir, n)); e != nil {
			t.Fatalf("missing %s: %v", n, e)
		}
	}
	data, e := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = DecodeCatalog(data); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), "testdata") {
		t.Fatal("fixture source leaked into catalog")
	}
}
