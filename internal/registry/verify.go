package registry

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type lookupFunc func(context.Context, string) ([]net.IP, error)
type verifyOptions struct {
	Client              *http.Client
	Lookup              lookupFunc
	AllowPrivateForTest bool
	TempDir             string
	Warn                func(string)
}

var errUnsupportedSourceHost = errors.New("source host commit lookup is unsupported")
var errReleaseMetadataAbsent = errors.New("matching GitHub release metadata is absent")

// Core Adapter Protocol v1 bounds a JSON frame to 8 MiB, excluding its newline.
const maxAdapterFrameBytes = 8 << 20

func VerifyArtifacts(ctx context.Context, plugins []Plugin, adapterRunner, storageRunner string) error {
	return verifyArtifacts(ctx, plugins, adapterRunner, storageRunner, verifyOptions{})
}

func verifyArtifacts(ctx context.Context, plugins []Plugin, adapterRunner, storageRunner string, opts verifyOptions) error {
	if len(plugins) == 0 {
		return nil
	}
	if adapterRunner == "" || storageRunner == "" {
		return errors.New("Core adapter and storage conformance tools are required for a nonempty catalog")
	}
	if opts.Lookup == nil {
		opts.Lookup = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	if opts.Warn == nil {
		opts.Warn = func(message string) { fmt.Fprintln(os.Stderr, "registryctl: warning:", message) }
	}
	if opts.Client == nil {
		opts.Client = newSafeHTTPClient(opts.Lookup, opts.AllowPrivateForTest)
	} else {
		client := *opts.Client
		client.CheckRedirect = safeRedirectPolicy(opts.Lookup, opts.AllowPrivateForTest)
		opts.Client = &client
	}
	for _, p := range plugins {
		for _, r := range p.Releases {
			var probe *string
			for i := range r.Artifacts {
				a := r.Artifacts[i]
				file, err := downloadArtifact(ctx, opts, a)
				if err != nil {
					return fmt.Errorf("artifact verification failed for %s %s %s/%s: %w", p.ID, r.Version, a.OS, a.Arch, err)
				}
				if a.OS == "linux" && a.Arch == "amd64" {
					probe = &file
				} else {
					removeStagedArtifact(file)
				}
			}
			if probe == nil {
				return fmt.Errorf("plugin %s release %s must provide linux/amd64 for descriptor conformance", p.ID, r.Version)
			}
			probeErr := probeExecutable(ctx, p, r, *probe, adapterRunner, storageRunner)
			removeStagedArtifact(*probe)
			if probeErr != nil {
				return fmt.Errorf("descriptor conformance failed for %s %s: %w", p.ID, r.Version, probeErr)
			}
			if err := verifySourceCommit(ctx, opts.Client, p.Repository, r.SourceCommit); err != nil {
				if errors.Is(err, errUnsupportedSourceHost) {
					opts.Warn("source commit lookup is unsupported for plugin " + p.ID + " version " + r.Version)
				} else {
					return fmt.Errorf("source commit verification failed for %s %s: %w", p.ID, r.Version, err)
				}
			}
			if err := verifyGitHubRelease(ctx, opts.Client, p.Repository, r); err != nil {
				if errors.Is(err, errReleaseMetadataAbsent) {
					opts.Warn("no matching GitHub release metadata found for plugin " + p.ID + " version " + r.Version)
				} else {
					return fmt.Errorf("release metadata verification failed for %s %s: %w", p.ID, r.Version, err)
				}
			}
		}
	}
	return nil
}

func newSafeHTTPClient(lookup lookupFunc, allowPrivate bool) *http.Client {
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: true, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid destination")
		}
		ips, err := lookup(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("destination resolution failed")
		}
		for _, ip := range ips {
			if !allowPrivate && !IsPublicIP(ip) {
				return nil, errors.New("non-public destination rejected")
			}
		}
		d := net.Dialer{Timeout: 10 * time.Second}
		for _, ip := range ips {
			conn, e := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, errors.New("destination connection failed")
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Minute}
	client.CheckRedirect = safeRedirectPolicy(lookup, allowPrivate)
	return client
}

func safeRedirectPolicy(lookup lookupFunc, allowPrivate bool) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("redirect limit exceeded")
		}
		if req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Fragment != "" || req.URL.Hostname() == "" {
			return errors.New("unsafe redirect rejected")
		}
		if _, err := resolvePublic(req.Context(), req.URL.Hostname(), lookup, allowPrivate); err != nil {
			return errors.New("unsafe redirect destination rejected")
		}
		return nil
	}
}

func resolvePublic(ctx context.Context, host string, lookup lookupFunc, allowPrivate bool) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if !allowPrivate && !IsPublicIP(ip) {
			return nil, errors.New("non-public IP")
		}
		return []net.IP{ip}, nil
	}
	ips, err := lookup(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("DNS lookup failed")
	}
	for _, ip := range ips {
		if !allowPrivate && !IsPublicIP(ip) {
			return nil, errors.New("non-public DNS result")
		}
	}
	return ips, nil
}

func downloadArtifact(ctx context.Context, opts verifyOptions, a Artifact) (string, error) {
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("unsafe artifact URL")
	}
	if _, err := resolvePublic(ctx, u.Hostname(), opts.Lookup, opts.AllowPrivateForTest); err != nil {
		return "", errors.New("artifact host is not public")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", errors.New("request could not be created")
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "integrated-recorder-registryctl/1")
	resp, err := opts.Client.Do(req)
	if err != nil {
		return "", errors.New("artifact download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("artifact server returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != a.Size {
		return "", errors.New("declared response size mismatch")
	}
	if resp.Uncompressed {
		return "", errors.New("compressed response rejected")
	}
	dir := opts.TempDir
	if dir == "" {
		dir = os.TempDir()
	}
	private, err := os.MkdirTemp(dir, "registry-artifact-")
	if err != nil {
		return "", errors.New("private staging could not be created")
	}
	if err = os.Chmod(private, 0700); err != nil {
		_ = os.RemoveAll(private)
		return "", errors.New("private staging permissions failed")
	}
	filePath := filepath.Join(private, "artifact")
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		_ = os.RemoveAll(private)
		return "", errors.New("private staging file could not be created")
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, MaxArtifactBytes+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || n != a.Size || n > MaxArtifactBytes || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		_ = os.RemoveAll(private)
		return "", errors.New("artifact size or SHA-256 verification failed")
	}
	if err := os.Chmod(filePath, 0700); err != nil {
		_ = os.RemoveAll(private)
		return "", errors.New("verified artifact mode could not be set")
	}
	return filePath, nil
}
func removeStagedArtifact(file string) {
	if file == "" {
		return
	}
	dir := filepath.Dir(file)
	_ = os.Remove(file)
	_ = os.Remove(dir)
}

type adapterReport struct {
	Passed          bool   `json:"passed"`
	ProtocolVersion int    `json:"protocol_version"`
	AdapterID       string `json:"adapter_id"`
}
type storageReport struct {
	Passed          bool   `json:"passed"`
	ProtocolVersion int    `json:"protocol_version"`
	ProviderID      string `json:"provider_id"`
	ProviderVersion string `json:"provider_version"`
}

func probeExecutable(ctx context.Context, p Plugin, r Release, binary, adapterRunner, storageRunner string) error {
	if p.Type == "storage" {
		ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, storageRunner, "--binary", binary, "--json")
		cmd.Env = probeEnvironment()
		var out boundedBuffer
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return errors.New("storage conformance failed")
		}
		var report storageReport
		if json.Unmarshal(out.Bytes(), &report) != nil || !report.Passed || report.ProtocolVersion != r.Protocol.Version || report.ProviderID != p.ID || report.ProviderVersion != r.Version {
			return errors.New("storage descriptor identity mismatch")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, adapterRunner, "--binary", binary, "--json", "--timeout", "10s")
	cmd.Env = probeEnvironment()
	var out boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return errors.New("source adapter conformance failed")
	}
	var report adapterReport
	if json.Unmarshal(out.Bytes(), &report) != nil || !report.Passed || report.ProtocolVersion != r.Protocol.Version || report.AdapterID != p.ID {
		return errors.New("source adapter descriptor identity mismatch")
	}
	return probeSourceVersion(ctx, binary, p.ID, r.Version, r.Protocol.Version)
}

type sourceEnvelope struct {
	ProtocolVersion int             `json:"protocol_version"`
	ID              string          `json:"id"`
	Result          json.RawMessage `json:"result"`
	Error           json.RawMessage `json:"error"`
}
type sourceDescriptor struct {
	ID              string `json:"id"`
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocol_version"`
}

func probeSourceVersion(ctx context.Context, binary, id, wantVersion string, wantProtocol int) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = probeEnvironment()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return errors.New("adapter launch failed")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return errors.New("adapter launch failed")
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return errors.New("adapter launch failed")
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	if _, err = io.WriteString(stdin, "{\"protocol_version\":1,\"id\":\"registryctl-describe\",\"method\":\"describe\",\"params\":{}}\n"); err != nil {
		return errors.New("adapter describe request failed")
	}
	lineCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		frame, readErr := readBoundedProtocolFrame(stdout)
		if readErr != nil {
			errCh <- readErr
			return
		}
		lineCh <- frame
	}()
	var line []byte
	select {
	case <-ctx.Done():
		return errors.New("adapter describe timed out")
	case <-errCh:
		return errors.New("adapter describe failed")
	case line = <-lineCh:
	}
	var env sourceEnvelope
	if json.Unmarshal(line, &env) != nil || env.ProtocolVersion != wantProtocol || env.ID != "registryctl-describe" || len(env.Error) > 0 || len(env.Result) == 0 {
		return errors.New("invalid adapter describe response")
	}
	var d sourceDescriptor
	if json.Unmarshal(env.Result, &d) != nil || d.ID != id || d.Version != wantVersion || d.ProtocolVersion != wantProtocol {
		return errors.New("adapter version identity mismatch")
	}
	return nil
}

func readBoundedProtocolFrame(r io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), maxAdapterFrameBytes+1)
	if !scanner.Scan() {
		return nil, errors.New("missing response")
	}
	if len(scanner.Bytes()) > maxAdapterFrameBytes {
		return nil, errors.New("oversized response")
	}
	return append([]byte(nil), scanner.Bytes()...), nil
}

type boundedBuffer struct {
	b     []byte
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.limit == 0 {
		b.limit = 1 << 20
	}
	if len(b.b)+len(p) > b.limit {
		return 0, errors.New("tool output limit exceeded")
	}
	b.b = append(b.b, p...)
	return len(p), nil
}
func (b *boundedBuffer) Bytes() []byte { return b.b }
func probeEnvironment() []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.TempDir(), "TMPDIR=" + os.TempDir(), "LANG=C"}
}

func verifySourceCommit(ctx context.Context, client *http.Client, repository, sha string) error {
	u, err := url.Parse(repository)
	if err != nil {
		return errors.New("invalid repository URL")
	}
	if !strings.EqualFold(u.Hostname(), "github.com") {
		return errUnsupportedSourceHost
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return errors.New("invalid GitHub repository URL")
	}
	endpoint := "https://api.github.com/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(strings.TrimSuffix(parts[1], ".git")) + "/commits/" + sha
	resp, err := githubGet(ctx, client, endpoint)
	if err != nil {
		return errors.New("GitHub commit lookup failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("source commit does not resolve")
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&commit) != nil || commit.SHA != sha {
		return errors.New("source commit response did not match pinned SHA")
	}
	return nil
}

func verifyGitHubRelease(ctx context.Context, client *http.Client, repository string, r Release) error {
	u, err := url.Parse(repository)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return nil
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	tag := "v" + r.Version
	endpoint := "https://api.github.com/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(repo) + "/releases/tags/" + url.PathEscape(tag)
	resp, err := githubGet(ctx, client, endpoint)
	if err != nil {
		return errors.New("GitHub release lookup failed")
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		endpoint = "https://api.github.com/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(repo) + "/releases/tags/" + url.PathEscape(r.Version)
		resp, err = githubGet(ctx, client, endpoint)
		if err != nil {
			return errors.New("GitHub release lookup failed")
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errReleaseMetadataAbsent
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub release lookup returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Assets []githubAsset `json:"assets"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload) != nil {
		return errors.New("invalid GitHub release metadata")
	}
	return verifyReleaseAssets(r.Artifacts, payload.Assets)
}

type githubAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

func verifyReleaseAssets(expected []Artifact, assets []githubAsset) error {
	for _, want := range expected {
		found := false
		for _, got := range assets {
			// Filename is the Core-installed executable name (which must be
			// stable across platforms), while a release host needs distinct
			// asset names such as adapter-linux-amd64 and adapter-linux-arm64.
			// The release URL and byte size identify the hosted asset here.
			if got.URL == want.URL && got.Size == want.Size {
				found = true
				break
			}
		}
		if !found {
			return errors.New("GitHub release does not list the approved artifact metadata")
		}
	}
	return nil
}

func githubGet(ctx context.Context, client *http.Client, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("request failed")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "integrated-recorder-registryctl/1")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return client.Do(req)
}

// BuildPages writes a static deployment tree. Only generated catalog, schemas,
// and a small landing page are copied; test fixtures are never published.
func BuildPages(plugins []Plugin, schemaDir, outDir string) error {
	catalog, err := Build(plugins)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("Pages output directory must be empty")
	}
	if err = os.WriteFile(filepath.Join(outDir, "catalog.json"), catalog, 0644); err != nil {
		return err
	}
	for _, name := range []string{"plugin-registry-v1.schema.json", "plugin-registry-v2.schema.json"} {
		b, e := os.ReadFile(filepath.Join(schemaDir, name))
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(outDir, name), b, 0644); e != nil {
			return e
		}
	}
	index := fmt.Sprintf("<!doctype html><html lang=\"en\"><meta charset=\"utf-8\"><title>Integrated Recorder Plugin Registry</title><h1>Integrated Recorder Plugin Registry</h1><p>Schema version %d · %d approved plugins</p><p><a href=\"catalog.json\">Catalog JSON</a> · <a href=\"https://github.com/integrated-recorder/core\">Core</a></p></html>\n", SchemaVersion, len(plugins))
	return os.WriteFile(filepath.Join(outDir, "index.html"), []byte(index), 0644)
}
