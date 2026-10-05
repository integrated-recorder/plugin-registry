# Canonical schema source

The normative Plugin Registry schemas are owned by the Integrated Recorder Core repository:

- Core: <https://github.com/integrated-recorder/core>
- Pinned Core commit: `912f2890a18c0d480b3a2ccd2aaae07bff76397c`
- v1 source: `docs/schemas/plugin-registry-v1.schema.json`
- v1 vendored SHA-256: `06d1d72781182f29c48f21fc4c8709ddb4a3c50fe8aaeb31c289ceef8fd9d031`
- v2 source: `docs/schemas/plugin-registry-v2.schema.json`
- v2 vendored SHA-256: `bf4105ed9dcf6971c9f1b2f446b67eef9cb4d4cf5f88836805fa0711c9e5eb48`
- v3 source: `docs/schemas/plugin-registry-v3.schema.json`
- v3 vendored SHA-256: `b282ceeda7911112f6634b2f0f2ab9c17901f5c571ea02f91752dc894708f68e`

The files in this directory are pinned copies for validation, compatibility
review, and static publication. They are not an independently maintained
specification. All three schema copies match the pinned Core commit above. The
v1 and v2 schemas are compatibility formats; the v3 schema adds publisher
metadata for trust-aware catalogs. A Core schema change must be proposed as an
explicit Registry pull request that updates the vendored schema, hashes, pinned
Core commit, and `registryctl check-core-schemas` baseline together. Reviewers
must inspect the Core change and resulting contract diff before merging.
