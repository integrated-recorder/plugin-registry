// Command registryctl validates and builds the official static Plugin Registry.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/integrated-recorder/plugin-registry/internal/registry"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "registryctl: %s\n", safeError(err))
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "validate":
		fs := flag.NewFlagSet("validate", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		dir := fs.String("plugins-dir", "plugins", "plugin source directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return usage()
		}
		plugins, err := registry.LoadPluginsV3(*dir)
		if err != nil {
			return err
		}
		fmt.Printf("valid Plugin Registry v3 source and v2 compatibility projection: %d plugins\n", len(plugins))
		return nil
	case "build":
		fs := flag.NewFlagSet("build", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		dir := fs.String("plugins-dir", "plugins", "plugin source directory")
		out := fs.String("output", "dist/catalog.json", "generated catalog path")
		outV3 := fs.String("output-v3", "dist/catalog-v3.json", "generated v3 catalog path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return usage()
		}
		plugins, err := registry.LoadPluginsV3(*dir)
		if err != nil {
			return err
		}
		if filepath.Clean(*out) == filepath.Clean(*outV3) {
			return fmt.Errorf("v2 and v3 output paths must differ")
		}
		data, err := registry.BuildV2Projection(plugins)
		if err != nil {
			return err
		}
		dataV3, err := registry.BuildV3(plugins)
		if err != nil {
			return err
		}
		if err = writeGenerated(*out, data); err != nil {
			return err
		}
		if err = writeGenerated(*outV3, dataV3); err != nil {
			return err
		}
		fmt.Printf("built deterministic v2 compatibility and v3 catalogs: %d plugins\n", len(plugins))
		return nil
	case "verify-artifacts":
		fs := flag.NewFlagSet("verify-artifacts", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		dir := fs.String("plugins-dir", "plugins", "plugin source directory")
		adapter := fs.String("adapter-conformance", "", "Core adapter-conformance executable")
		storage := fs.String("storage-conformance", "", "Core storage-provider-conformance executable")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return usage()
		}
		plugins, err := registry.LoadPluginsV3(*dir)
		if err != nil {
			return err
		}
		if len(plugins) == 0 {
			fmt.Println("artifact verification: no production artifacts (success)")
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		if err = registry.VerifyArtifacts(ctx, registry.V2Projection(plugins), *adapter, *storage); err != nil {
			return err
		}
		fmt.Printf("verified artifacts and protocol descriptors for %d plugins\n", len(plugins))
		return nil
	case "check-immutability":
		fs := flag.NewFlagSet("check-immutability", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		base := fs.String("base", "", "base Git ref")
		dir := fs.String("plugins-dir", "plugins", "plugin source directory")
		repo := fs.String("repo-dir", ".", "Git repository root")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return usage()
		}
		if *base == "" {
			return fmt.Errorf("--base is required")
		}
		if err := registry.CheckImmutabilityV3(*repo, *base, *dir); err != nil {
			return err
		}
		fmt.Printf("release history is immutable relative to %s\n", *base)
		return nil
	case "check-core-schemas":
		fs := flag.NewFlagSet("check-core-schemas", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		core := fs.String("core-dir", "", "Core checkout at pinned SHA")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 || *core == "" {
			return usage()
		}
		return checkCoreSchemas(*core)
	case "pages":
		fs := flag.NewFlagSet("pages", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		dir := fs.String("plugins-dir", "plugins", "plugin source directory")
		schemas := fs.String("schemas-dir", "schemas", "vendored schema directory")
		out := fs.String("output", "site", "Pages staging directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return usage()
		}
		plugins, err := registry.LoadPluginsV3(*dir)
		if err != nil {
			return err
		}
		if err = registry.BuildPagesV3(plugins, *schemas, *out); err != nil {
			return errorsSafe("Pages site could not be built")
		}
		fmt.Printf("built static Pages tree with v2 compatibility and v3 catalogs: %d approved plugins\n", len(plugins))
		return nil
	default:
		return usage()
	}
}

func checkCoreSchemas(coreDir string) error {
	out, err := exec.Command("git", "-C", coreDir, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(out)) != registry.PinnedCoreCommit {
		return fmt.Errorf("Core checkout must be pinned to %s", registry.PinnedCoreCommit)
	}
	want := map[string]string{"plugin-registry-v1.schema.json": "06d1d72781182f29c48f21fc4c8709ddb4a3c50fe8aaeb31c289ceef8fd9d031", "plugin-registry-v2.schema.json": "bf4105ed9dcf6971c9f1b2f446b67eef9cb4d4cf5f88836805fa0711c9e5eb48", "plugin-registry-v3.schema.json": "b282ceeda7911112f6634b2f0f2ab9c17901f5c571ea02f91752dc894708f68e"}
	for name, digest := range want {
		vendored, err := os.ReadFile(filepath.Join("schemas", name))
		if err != nil {
			return fmt.Errorf("vendored schema %s is unavailable", name)
		}
		h := sha256.Sum256(vendored)
		if hex.EncodeToString(h[:]) != digest {
			return fmt.Errorf("vendored schema %s differs from the recorded schema hash", name)
		}
		source, err := exec.Command("git", "-C", coreDir, "show", registry.PinnedCoreCommit+":docs/schemas/"+name).Output()
		if err != nil {
			if name == "plugin-registry-v3.schema.json" {
				workspaceCopy, workspaceErr := os.ReadFile(filepath.Join(coreDir, "docs", "schemas", name))
				if workspaceErr == nil && !bytesEqual(workspaceCopy, vendored) {
					return fmt.Errorf("vendored v3 schema differs from the uncommitted Core schema copy")
				}
				fmt.Println("v3 Core schema source commit is pending; provenance remains unpinned")
				continue
			}
			return fmt.Errorf("Core schema %s is unavailable at the pinned commit", name)
		}
		if !bytesEqual(source, vendored) {
			return fmt.Errorf("vendored schema %s differs from the pinned Core schema", name)
		}
	}
	fmt.Println("vendored schemas match their recorded hashes and pinned Core sources where available")
	return nil
}

func writeGenerated(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return errorsSafe("output directory could not be created")
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return errorsSafe("catalog could not be written")
	}
	return nil
}
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func safeError(err error) string {
	if err == nil {
		return "operation failed"
	}
	return err.Error()
}
func errorsSafe(s string) error { return fmt.Errorf("%s", s) }
func usage() error {
	return fmt.Errorf("usage: registryctl {validate|build|verify-artifacts|check-immutability|check-core-schemas|pages} [flags]")
}
