# One-time GitHub administration

The initial repository setup requires an organization administrator. The source repository cannot
apply these settings by committing files, and a workflow must not claim they are active until an
administrator confirms them.

1. Create public `integrated-recorder/plugin-registry` with `main` as its default branch and the
   description: “Curated plugin registry for Integrated Recorder source and storage plugins.”
2. Enable GitHub Pages with **GitHub Actions** as the publishing source. The canonical v3 catalog
   URL is `https://integrated-recorder.github.io/plugin-registry/catalog-v3.json`; the v2
   compatibility URL is `https://integrated-recorder.github.io/plugin-registry/catalog.json`.
3. Protect `main`: require pull requests and successful `validate` workflow checks; in current
   solo-maintainer mode set required approving reviews to zero. Enforce the rules for administrators,
   block direct pushes, and prohibit force pushes and branch deletion. When a second active trusted
   maintainer joins, restore at least one required approving review while retaining the other rules.
4. Confirm `.github/CODEOWNERS` resolves to the intended organization owner. Add an actual team only
   after that team exists and has the right reviewers.
5. After the Owncast Registry PR is merged and Pages deployment succeeds, verify both
   HTTPS catalog URLs return HTTP 200. The v3 catalog must be schema version 3 and include the
   Registry-approved Owncast 0.2.0 entry; the v2 compatibility catalog must be schema version 2 and include
   the same release without publisher metadata. Then configure Core's `IR_PLUGIN_REGISTRY_URL` to
   the canonical v3 URL as a separate operator/product decision; this repository does not change
   Core defaults.

Until the Pages configuration is enabled and checked, the URL is a target, not a claim of an active
publication.
