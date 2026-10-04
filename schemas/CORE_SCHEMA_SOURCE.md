# Canonical schema source

The normative Plugin Registry schemas are owned by the Integrated Recorder Core repository:

- Core: <https://github.com/integrated-recorder/core>
- Compatibility baseline: `3cf2c90280873f98f5120f67e39516c5af8abc04`
- v1 source: `docs/schemas/plugin-registry-v1.schema.json`
- v1 vendored SHA-256: `06d1d72781182f29c48f21fc4c8709ddb4a3c50fe8aaeb31c289ceef8fd9d031`
- v2 source: `docs/schemas/plugin-registry-v2.schema.json`
- v2 vendored SHA-256: `bf4105ed9dcf6971c9f1b2f446b67eef9cb4d4cf5f88836805fa0711c9e5eb48`

The files in this directory are pinned copies for validation, compatibility
review, and static publication. They are not an independently maintained
specification. A Core schema change must be proposed as an explicit Registry
pull request that updates both vendored schema files, hashes, compatibility
commit, and the `registryctl check-core-schemas` baseline together. Reviewers
must inspect the Core change and resulting contract diff before merging.
