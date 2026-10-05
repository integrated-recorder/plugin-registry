This repository is the official approval and distribution index for Integrated Recorder plugins.

The Registry approves exact released executable bytes for the Core Runtime Host. Plugin source and
builds remain in their own repositories; this repository never clones or builds plugin source.

## Production catalog

The v3 source catalog now proposes the real Owncast 0.2.0 release from
`integrated-recorder/source.owncast`, with immutable source commit, exact artifact size and
SHA-256, and a matching Protocol v1 descriptor. The Registry PR and required review/CI must pass
before this entry is published. `storage.local` is the bundled reference Storage Provider in Core
and is never distributed by this Registry.

After GitHub Pages is enabled for this repository, the canonical catalog URL is:

`https://integrated-recorder.github.io/plugin-registry/catalog-v3.json`

The v2 compatibility catalog remains available at
`https://integrated-recorder.github.io/plugin-registry/catalog.json` while older Core runtimes
consume v2.

The Registry is approval metadata, not a runtime dependency for already-installed plugins. If the
Registry is unavailable, installed immutable plugin artifacts continue to work.

## How approval works

Plugin repositories build and publish executable releases. GitHub Releases or another HTTPS host
transports those bytes. A reviewed Registry change pins the plugin identity, type, version, source
commit, Protocol family/version, platform, filename, exact size, and SHA-256. Runtime Host downloads
the bytes, checks exact size and digest, probes the executable descriptor, and imports it through
the existing immutable plugin lifecycle. A changed release asset with an unchanged Registry digest
is rejected by Core.

Registry merges require explicit human review. See [CONTRIBUTING.md](CONTRIBUTING.md),
[SECURITY.md](SECURITY.md), and [docs/ADMIN_SETUP.md](docs/ADMIN_SETUP.md).

The Registry builder reads one v3 JSON source file per plugin from `plugins/`, including an explicit
`publisher.kind` of `first_party` or `third_party`. It generates canonical `dist/catalog-v3.json`
and a v2-compatible `dist/catalog.json` projection that omits publisher-only metadata. The generated
catalogs are deployment artifacts, not source of truth, and are not committed. Both builds are
deterministic. First-party entries must use a repository owned by the `integrated-recorder` GitHub
organization; third-party repositories may be hosted elsewhere. Source ID `hls` and storage ID
`local` are reserved.

```sh
go test ./...
go run ./cmd/registryctl validate
go run ./cmd/registryctl build
go run ./cmd/registryctl verify-artifacts
```

An empty catalog is valid and `verify-artifacts` succeeds without network access when there are no
production entries.

## License

This repository follows the Integrated Recorder ecosystem's AGPL-3.0 licensing policy. See
[LICENSE](LICENSE).
