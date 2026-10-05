# Contributing plugins

Every catalog change is an approval decision. Open a pull request; do not edit generated output by
hand. Registry maintainers review and merge the PR. A merge approves the precise executable bytes
listed in the source file.

## Add a release

1. Build and test the executable in its plugin repository and publish a real release. Registry CI
   never clones or builds plugin source.
2. Publish the supported Linux artifacts (`linux/amd64` is required for descriptor probing;
   `linux/arm64` is strongly expected for the official container targets). Publish other supported
   platforms only when they are real binaries.
3. Record the immutable lowercase 40-hex source commit, release version, protocol family/version,
   HTTPS asset URL, exact filename, byte size, and lowercase SHA-256.
4. Add or update one file named `source.<id>.json` or `storage.<id>.json` in `plugins/`. Each file
   contains exactly one Plugin Registry v3 plugin object and an explicit publisher classification:
   `first_party` or `third_party`. First-party entries must link to a repository owned by the
   `integrated-recorder` GitHub organization. Third-party repositories may be external. No signed
   query-string URLs are accepted.
5. Point any non-empty subset of `stable`, `beta`, and `development` channels only at a release
   present in that file.
6. Submit a PR. CI validates v3 structure, the v2 compatibility projection, append-only history,
   downloaded bytes, GitHub source commit,
   and the Protocol v1 executable descriptor. It does not build the submitted plugin.
7. Reviewers inspect the repository identity, license, source commit/tag, release relation, hashes,
   protocol descriptor, requested network/filesystem authority, secret use, and unexpected
   dependencies. Approval must be explicit before merge.
8. Merge to `main`; the Pages workflow publishes the generated static catalog.

Releases are append-only. Never remove a merged version or change the approved bytes for an existing
`(plugin ID, version, OS, architecture)`. Publish a new plugin version to replace an artifact.
Channel pointers may move between versions already present in that plugin file. Display name and
repository metadata may be corrected when that does not rewrite release identity.

## Source format

The v1, v2, and v3 wire contracts are vendored from the exact Core commit recorded in
[`schemas/CORE_SCHEMA_SOURCE.md`](schemas/CORE_SCHEMA_SOURCE.md). Schema updates require a separate
explicit Registry PR that pins the final Core commit and records the vendored schema hashes, with
review of the corresponding Core change.

Official repositories are encouraged to use `source.<platform>` or `storage.<backend>` names.
Third-party repositories are permitted; the schema does not require a repository to belong to the
Integrated Recorder organization.

The canonical source of truth is v3. Pages also publishes a v2 projection as `catalog.json` for
older Core runtimes; publisher metadata is intentionally absent from that compatibility document.
`storage.local` is the bundled mandatory Core provider. It is not remotely installable and must not
be added to either catalog. Source ID `hls` is reserved.

Only real released plugins may enter the production catalog. Examples and test executables belong
only in `testdata/` and are never included in the Pages output.
