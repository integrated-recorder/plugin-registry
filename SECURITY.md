# Security and trust model

The Registry is the approval authority for exact plugin release artifacts. A successful review and
merge approves an artifact identity, version, platform, byte size, SHA-256, and declared Protocol
family/version. Core verifies those values and the executable descriptor before immutable import.
The hosting service (GitHub Release/CDN) only transports the bytes; it is not the trust root.

This approval does **not** establish that a plugin is safe, reproducible from its source, or free of
malicious behavior. Native plugins are trusted executable code. They are not sandboxed and may
receive the network, filesystem, and secrets required by their declared operation. Registry review
cannot prove that approved source produced the binary.

The v1/v2 model does not defend against compromise of this Registry repository, GitHub accounts,
maintainer credentials, or an authorized reviewer. It does not prevent rollback/freeze attacks and
does not use publisher PKI, signed catalog snapshots, or TUF-style metadata. The current operational
trust root is the `main` branch of `integrated-recorder/plugin-registry`, its GitHub access controls,
required validation, and human review. Signed snapshots and stronger rollback protection are future
work, not guarantees of this Registry.

Artifact URLs must be HTTPS and contain no userinfo, query string, or fragment. CI streams each
artifact into a private temporary file, enforces the Core 512 MiB ceiling, checks exact size and
SHA-256, and probes the Linux/amd64 executable with the Core-owned Protocol conformance runner.
Other listed platforms receive the same URL/size/hash verification. Artifact bodies and arbitrary
tool stderr are not emitted to logs.

Report suspected malicious catalog entries or credential compromise privately to the organization
maintainers before public disclosure.
