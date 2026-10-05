# Security and trust model

The Registry is the approval authority for exact plugin release artifacts. A successful PR merge
after required validation approves an artifact identity, version, platform, byte size, SHA-256, and
declared Protocol family/version. Core verifies those values and the executable descriptor before
immutable import.
The hosting service (GitHub Release/CDN) only transports the bytes; it is not the trust root.

This approval does **not** establish that a plugin is safe, reproducible from its source, or free of
malicious behavior. Native plugins are trusted executable code. They are not sandboxed and may
receive the network, filesystem, and secrets required by their declared operation. Registry
approval cannot prove that approved source produced the binary.

The Registry currently operates in solo-maintainer mode. Every change must use a pull request and
pass required automated validation, including immutable artifact verification. An independent
approving review is not required while there is one maintainer; the project intends to require at
least one approval after a second active trusted maintainer joins. PR protection, required CI,
administrator enforcement, direct-push protection, force-push prohibition, and branch-deletion
protection remain in effect.

The v1/v2 compatibility and v3 models do not defend against compromise of this Registry repository,
GitHub accounts, or maintainer credentials. It does not prevent rollback/freeze attacks and does
not use publisher PKI, signed catalog snapshots, or TUF-style metadata. The current operational
trust root is the `main` branch of `integrated-recorder/plugin-registry`, its GitHub access controls,
and required validation. Signed snapshots and stronger rollback protection are future work, not
guarantees of this Registry.

Catalog v3 classifies each plugin as `first_party` or `third_party`. A first-party repository must
be owned by the `integrated-recorder` GitHub organization; third-party repositories may be external.
This classification does not add publisher authentication or attest source-to-binary
reproducibility. The v3 schema is pinned to the committed Core source revision and SHA-256 recorded
in [`schemas/CORE_SCHEMA_SOURCE.md`](schemas/CORE_SCHEMA_SOURCE.md).

Artifact URLs must be HTTPS and contain no userinfo, query string, or fragment. CI streams each
artifact into a private temporary file, enforces the Core 512 MiB ceiling, checks exact size and
SHA-256, and probes the Linux/amd64 executable with the Core-owned Protocol conformance runner.
Other listed platforms receive the same URL/size/hash verification. Artifact bodies and arbitrary
tool stderr are not emitted to logs.

Report suspected malicious catalog entries or credential compromise privately to the organization
maintainers before public disclosure.
