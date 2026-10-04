# One-time GitHub administration

The initial repository setup requires an organization administrator. The source repository cannot
apply these settings by committing files, and a workflow must not claim they are active until an
administrator confirms them.

1. Create public `integrated-recorder/plugin-registry` with `main` as its default branch and the
   description: “Curated plugin registry for Integrated Recorder source and storage plugins.”
2. Enable GitHub Pages with **GitHub Actions** as the publishing source. The expected catalog URL
   is `https://integrated-recorder.github.io/plugin-registry/catalog.json`.
3. Protect `main`: require pull requests, at least one approval, successful `validate` workflow
   checks, and prohibit force pushes and branch deletion.
4. Confirm `.github/CODEOWNERS` resolves to the intended organization owner. Add an actual team only
   after that team exists and has the right reviewers.
5. After the first successful Pages deployment, verify the HTTPS catalog returns HTTP 200 and the
   empty v2 document. Then configure Core's `IR_PLUGIN_REGISTRY_URL` to the canonical URL as a
   separate operator/product decision; this repository does not change Core defaults.

Until the Pages configuration is enabled and checked, the URL is a target, not a claim of an active
publication.
