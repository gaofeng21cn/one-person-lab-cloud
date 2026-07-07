# Latest Published Docs

Owner: `one-person-lab-cloud`
Purpose: `latest_published_docs_output_boundary`
State: `active_support`
Machine boundary: `docs/site/latest/` is local generated output for GitHub
Pages. It is not tracked on `main`.
Source truth stays in `docs/whitepapers/` and verification records under
`docs/delivery/`.

This repo publishes one current public whitepaper copy, not one copy per
release. Build locally, then publish the final user-facing files to the
`gh-pages` branch with `bash scripts/publish-docs-latest.sh`. GitHub Actions
does not rebuild these docs.

Generated output:

- `docs/site/latest/whitepapers/opl-cloud-whitepaper.html`
- `docs/site/latest/whitepapers/opl-cloud-whitepaper.pdf`

Do not commit `docs/site/latest/` on `main`. Rebuild it with
`node --experimental-strip-types scripts/build-opl-cloud-whitepaper.ts`;
publish it with `bash scripts/publish-docs-latest.sh`. The publish script
filters out process files such as generated Markdown and JSON.
