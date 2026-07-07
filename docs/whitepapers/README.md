# Whitepapers

Owner: `one-person-lab-cloud`
Purpose: `whitepaper_source_root`
State: `active`
Machine boundary: Source prose for public whitepapers. Generated HTML/PDF live
under local `docs/site/latest/` and are published from the local build to the
latest GitHub Pages copy, not tracked release-by-release files on `main`.

OPL-family whitepapers use the shared local builder
`scripts/opl-whitepaper-builder.ts`; this repo's
`scripts/build-opl-cloud-whitepaper.ts` is only the Cloud-specific
configuration wrapper for the common Pandoc/XeLaTeX style profile.

Current source:

- `opl-cloud-whitepaper.md`
