# NFe.io authoritative image-version handoff

Date: 2026-09-06

## Scope

Make every official `integration-nfeio` image advertise the adapter version from
the canonical `providers/nfeio/adapter.AdapterVersion` source. This change does
not tag, deploy, or mutate production.

## Starting evidence

- Base commit: `8f45f19cd649a4aa745924f4f75de56fb31839da` (merged PR #6).
- Its release run succeeded:
  <https://github.com/dakasa-yggdrasil/integration-nfeio/actions/runs/34018601952>.
- Published SHA tag: `sha-8f45f19`.
- Published OCI index digest:
  `sha256:a4231a485b128bfcd7436f38a7c06b391d183c360c249472104b1a5d65cf4ebb`.
- That build targeted `linux/amd64` and `linux/arm64`.
- No Git tag, GitHub release, or GHCR tag named `v3.1.2` existed at intake.
- The release log showed the binary linked with `main.Version=dev`. The source
  contract and `/rpc/describe` still reported `3.1.2`, because
  `DescribeHandler` called `Describe()`, which read the separate
  `AdapterVersion` constant. SDK v0.9.1 stores `Config.Version` but does not
  currently put it on the HTTP wire. The binary identity and live contract
  could therefore drift silently.

Do not treat the intake digest as the corrected artifact. It predates this
pipeline fix.

## Implemented contract

- `AdapterVersion` is now the single linkable variable used by:
  - `--version`;
  - `sdkadapter.Config.Version`;
  - `Describe().adapter.version`.
- `scripts/adapter-version.sh` reads and validates exactly one semantic version
  from that canonical source.
- The Docker build links that exact variable. A missing build argument falls
  back to the canonical source; a divergent override fails the build.
- CI proves link propagation with sentinel `0.0.0-ci-link-test` by starting the
  built adapter and reading the framed `/rpc/describe` response.
- CI also starts the canonical container image and requires both `--version`
  and `/rpc/describe` to equal the source version and not `dev`.
- The release workflow passes the source version into Docker and the OCI
  version label.
- Git tag pushes and requested release tags must equal `v<AdapterVersion>`.
- Manual releases fail unless the selected ref is `main` or an existing tag.

## Validation completed

All of the following passed from the isolated worktree:

```text
GOWORK=off GOMODCACHE=/tmp/nfeio-modcache.fcku0F go test ./... -count=1 -timeout 5m
GOWORK=off GOMODCACHE=/tmp/nfeio-modcache.fcku0F go vet ./...
GOWORK=off GOMODCACHE=/tmp/nfeio-modcache.fcku0F go run ./cmd/lint-action-catalog
GOWORK=off GOMODCACHE=/tmp/nfeio-modcache.fcku0F go run ./cmd/validate-templates ./manifest/templates/
actionlint .github/workflows/ci.yml .github/workflows/release.yml
bash -n scripts/adapter-version.sh
git diff --check
```

Runtime/build proofs:

- Sentinel binary: `--version=0.0.0-ci-link-test` and
  `/rpc/describe.adapter.version=0.0.0-ci-link-test`.
- Canonical image with explicit `VERSION=3.1.2`: `--version=3.1.2` and
  `/rpc/describe.adapter.version=3.1.2`.
- Canonical image without a build argument: `--version=3.1.2`.
- Deliberate `VERSION=9.9.9` build: rejected because the source is `3.1.2`.

Only synthetic CI credentials were used. No NFe.io request was made.

## Branch handoff

- Worktree:
  `/Users/dakasa/projects/dakasa/.codex-worktrees/integration-nfeio-authoritative-build-version-20260906`
- Branch: `codex/nfeio-authoritative-build-version-20260906`
- Base: `origin/main` at
  `8f45f19cd649a4aa745924f4f75de56fb31839da`
- Required author: `Giomaster <giovanni.martins@dakasa.me>`
- Merge, tag, release dispatch, and production deployment remain explicitly out
  of scope for this branch handoff.

## Post-merge release gate

1. Wait for CI, CodeQL, and the multi-architecture Release run for the merge
   commit to succeed.
2. Resolve the new `sha-<merge-short-sha>` tag and OCI index digest from GHCR.
3. Verify the OCI label is `3.1.2`, both `linux/amd64` and `linux/arm64` are
   present, and the built adapter reports `3.1.2` through `/rpc/describe`.
4. Deploy only by the new immutable digest. Do not reuse
   `sha256:a4231a485b128bfcd7436f38a7c06b391d183c360c249472104b1a5d65cf4ebb`.
5. Stop the rollout if the release reports `dev`, the source/OCI/Describe
   versions disagree, either target platform is absent, or any required check
   fails.
6. A semantic `v3.1.2` tag is not required for the SHA rollout. Creating one is
   a separate release mutation and requires explicit authorization.
