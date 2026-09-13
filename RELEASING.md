# Releasing

kmap releases by pushing a tag. Nothing is built or uploaded by hand, and nothing in the repo hardcodes a version.

## Versioning

- Tags use the **`v` prefix** and semver: `v0.1.0`, `v0.2.0`, `v1.0.0`.
- Stay pre-1.0 (`v0.x`) until the config schema (`version: 1`) and the alias grammar (the three shapes under `aliases:` — a bare name, an object, or a per-environment map) are frozen for a 1.0 promise.

## Cut the release

Pushing a `vX.Y.Z` tag runs [`.github/workflows/release.yml`](.github/workflows/release.yml), which cross-compiles four targets (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`), packages each as a tarball alongside `README.md` and `LICENSE`, computes one `checksums.txt` covering all four, and attaches all five files to the GitHub release. There is no local build step to run first.

```sh
git tag -a vX.Y.Z -m "kmap vX.Y.Z"
git push origin vX.Y.Z
```

Nothing in the source tree records the version being released: `cmd/version.go` reports whatever the workflow's `-ldflags "-X ...cmd.version=vX.Y.Z"` injected at build time. Unlike AutoActivator, there is no docs bump to make before tagging — no file anywhere quotes the current version for a human to keep in sync.

## After the workflow finishes

Verify the release carries all five assets — four tarballs plus `checksums.txt` — before announcing it:

```sh
gh release view vX.Y.Z -R aymenkrifa/kmap --json assets -q '.assets[].name'
```

## The site

kmap's site publishes from `main`'s `/docs` via GitHub Pages, so pushing to `main` redeploys it — independently of any tag. The masthead's version badge is read from the GitHub API at runtime, so it needs no bump either.
