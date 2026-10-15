# CI and release automation

`workflows/ci.yml` runs the repository gate on native Linux amd64 and arm64, validates workflows, builds and checks validator images without networking, and uploads GoReleaser snapshot archives. It is also a reusable workflow called by `workflows/release.yml`. Go comes from `go.mod`; check tools, GoReleaser, and Buildx have explicit versions. Actions use immutable commit SHAs.

The native quality jobs also install pinned Node and npm, compile and test the UI, and prepare matching UI trees. Snapshot and release packaging consume those trees, with manifests stamped to the Go release identity. The `terminal-archives` jobs verify and extract the final snapshot archives on both native Ubuntu 26.04 targets, then run real PTY checks on the host and the pinned stock Debian 10 baseline from `ui/runtime.json`. Baseline networking is disabled and host Node is unavailable on `PATH`; prepared trees alone do not replace final archive proof. Release publication waits for the complete reusable CI workflow.

CI has read permissions and uses the `pull_request` event without publishing secrets. No live Telegram crawl, Cloudflare registration, or key attachment belongs in CI. The network disabled smoke script uses synthetic input and removes only its own container.

Release tags must match the documented stable or prerelease syntax and belong to main history. Reuse CI for the tagged commit before publishing. Only image and release jobs receive package or contents write permission. Use `GITHUB_TOKEN`; do not add long lived publishing credentials without a concrete need.

Build each validator on its native architecture, verify it, and record its registry digest. Assemble the shared version tag from those digests, then verify anonymous registry access before publishing the binary release. GHCR package visibility is a one time repository owner setting. Do not silently publish an inaccessible dependency or update a floating latest image tag.

`.goreleaser.yaml` owns CLI archives, documentation, checksums, version metadata, and release notes. The worker is distributed only inside the validator image. `.release/` is ignored temporary input holding image digests for publication. Do not reuse a published release tag or infer live entitlement proof from offline checks.

The user and maintainer instructions are in [docs/releasing.md](../docs/releasing.md). Verify workflow edits with `actionlint`, `goreleaser check`, and a local snapshot. A local rehearsal does not establish that hosted Actions or registry publication succeeded.

_Drafted by /sync from the introducing change, worth a quick human pass._
