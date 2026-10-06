# Integrated Recorder Plugin Registry

[한국어](README.md) | **English**

This repository is the source of truth for the official Integrated Recorder Plugin Registry. It tracks exact artifact identities and distribution metadata for approved plugin releases.

## Published catalog

Owncast `0.2.0` is listed as a first-party source plugin in the official catalog.

- v3 catalog: [catalog-v3.json](https://integrated-recorder.github.io/plugin-registry/catalog-v3.json)
- v2 compatibility catalog: [catalog.json](https://integrated-recorder.github.io/plugin-registry/catalog.json)

v3 includes publisher metadata. The v2 catalog is a projection for compatibility with existing Core runtimes. `storage.local` is Core's bundled reference Storage Plugin and is not distributed through this Registry.

## Role and boundaries

Plugin repositories own their source and build/release executables. GitHub Releases or another HTTPS artifact host transports the bytes. This Registry approves plugin ID/type/version, protocol, source commit, and each platform's URL, filename, size, and SHA-256 through its change process. The Core Runtime Host verifies downloaded bytes and the descriptor, then imports them through its immutable lifecycle.

This repository does not clone or build plugin source. Registry approval means approval of distribution metadata; it does not guarantee that a plugin is safe or malware-free, provide a sandbox, or prove that a build is reproducible from source. The Registry does not need to remain online for already-installed plugins to run.

## Source of truth and validation

Human-maintained inputs are one file per plugin under `plugins/*.json`. The builder deterministically generates a v3 catalog and a v2 compatibility catalog without publisher-only fields. Generated output is a deployment artifact, not edited by hand. A release identity cannot change artifact metadata or digest under the same version; publish a new plugin version when a release must be corrected.

```sh
go test ./...
go run ./cmd/registryctl validate
go run ./cmd/registryctl build
go run ./cmd/registryctl verify-artifacts
go run ./cmd/registryctl check-core-schemas
go run ./cmd/registryctl check-immutability
```

An empty catalog is valid; with no plugin entries, artifact verification succeeds without network requests. See [CORE_SCHEMA_SOURCE.md](schemas/CORE_SCHEMA_SOURCE.md) for the Core schema baseline and vendoring process.

## Submitting changes and operations

The Registry currently operates in solo-maintainer mode. All changes must go through a PR and pass required automated and immutable-artifact validation. An independent approving review is not currently required. This does not mean that a separate human review occurred. When a second trusted maintainer becomes active, the project intends to restore at least one required approval. See [CONTRIBUTING](CONTRIBUTING.md), [SECURITY](SECURITY.md), and [Admin setup](docs/ADMIN_SETUP.md) for details.

Official first-party plugins must use repositories owned by the `integrated-recorder` organization. Third-party plugins may use external repositories. Source ID `hls` and Storage ID `local` are reserved for Core-bundled identities.

## Ecosystem role and license

- [Integrated Recorder Core](https://github.com/integrated-recorder/core) — Runtime that verifies and installs artifacts
- [Adapter SDK for Go](https://github.com/integrated-recorder/adapter-sdk-go) — Source Plugin authoring SDK
- [source.owncast](https://github.com/integrated-recorder/source.owncast) — first-party Owncast integration in the official Registry
- [source.soop](https://github.com/integrated-recorder/source.soop) — under development/experimental, not ready for stable Registry distribution

See [LICENSE](LICENSE) for this repository's license.
