# Releasing Unobin

Releases use the same version for the CLI and the Go module. A pushed release
tag starts the workflow. It checks the changelog, runs the full Go test suite and
lint, builds the archives, and publishes a GitHub Release with the matching
changelog entry as its release notes.

Release tags use `vMAJOR.MINOR.PATCH`, without a suffix. They also start the
existing documentation workflow. Development tags such as `v0.12.0-a.7` do not
publish releases or create changelog version sections. Keep their changes under
`Unreleased` until the next release.

## Local checks and builds

Install Docker and GNU Make. The Makefile defines the Go and golangci-lint image
versions and runs the same containers locally and in GitHub Actions.
Packaging also needs `tar` and either `sha256sum` or `shasum` (Perl's SHA utility).

```sh
make test
make lint
make release VERSION=v0.12.0
```

`make test` includes the compiled factory and external consumer tests. It does
not use Go's short-test mode. The build container includes Node, npm, and Python
for the editor and browser tests. Keep the Go image version in the Makefile
consistent with `go.mod` when updating Go.

Build one platform with:

```sh
make release-one VERSION=v0.12.0 OS=linux ARCH=amd64
make check-archive VERSION=v0.12.0 OS=linux ARCH=amd64
```

Run `make check-cli-version` with the same arguments to extract the archive and
check its version on a matching host. Use `make check-release-assets` with a
version to verify all four archives. GitHub Actions calls these same targets.

Archives and checksum files are written to `_output/release/`. Each archive
contains `unobin`, `LICENSE`, `README.md`, and `CHANGELOG.md`. The supported
platforms are Linux and macOS, each on amd64 and arm64. The binary reports the
requested version through `unobin version` and uses it for generated factories'
Unobin dependency.

Local builds also accept development versions and do not need an existing tag
or changelog entry. They do not publish anything. Output directories include
the version so successive builds cannot reuse a binary stamped with another
version.

## Library API compatibility

The library implementation API has its own `major.minor` identifier. API `1.0`
is the baseline for the typed resource definitions and lifecycle contracts.
CLI releases, library releases, and the minimum Go version retain their own
version numbers. A release number does not determine an API identifier.

Keep `pkg/libraryapi/descriptor.json` unchanged for fixes that restore an
existing contract. An additive contract change can increase its minor. Preserve
the earlier contracts within that major, including Go signatures, registration
records, source inspection, configuration, and lifecycle behavior. A deliberate
breaking change requires a new API major. Advertise only contracts that the
runtime implements, and set `generator-api` to one of those advertised entries.

Run the unchanged library in `tests/e2e/testdata/modules/e2elib` through
`TestCompiledCases/lifecycle` for every runtime release implementing API `1.x`.
Its declaration remains `1.0`. The case compiles a factory and checks create,
update, replacement, no-op, and delete behavior. The
`TestLifecycleLibraryAPIBaseline` test also checks that injected descriptors
`1.0` and `1.1` accept this same declaration. When a real
release first advertises a newer minor, its compiled lifecycle run must still
use this fixture unchanged.

The full suite includes dependency candidates rejected for newer APIs, exact
and transitive floors, configuration-only dependencies, locked commits, local
replacements, and historical source with no declaration. Run `make test` before
publishing. The tested CLI/library combinations provide evidence for the API
promise; libraries can also use later compatible releases without a new record.

Library publishers migrate implementations to the typed baseline, declare the
minimum API in each library and configuration package, run compiled consumer
tests, and publish new tags. Preserve existing tags and commits. Historical
libraries without a declaration remain unknown to the compatibility checks.
See [library author guidance](docs/go-sdk/libraries.md#maintaining-compatibility).

## Publish a release

1. Choose an unused release version. Review changes since the previous release
   tag and update `CHANGELOG.md`. Use `Added`, `Changed`, `Fixed`, and `Removed`
   sections as needed, and describe breaking changes and migration steps explicitly.
2. Move the `Unreleased` entries into a dated heading without the tag's leading
   `v`, for example `## [0.12.0] - 2026-09-28`. Keep an empty `Unreleased`
   section for future changes. Add the release link at the bottom and update the
   `Unreleased` comparison link to start at the new tag.
3. Run the checks and build commands above, then check the release entry:

   ```sh
   make check-release VERSION=v0.12.0
   ```

4. Commit the release changes and wait for branch CI to pass. Tag that commit
   and push the branch and tag:

   ```sh
   git tag -a v0.12.0 -m 'Release v0.12.0'
   git push origin HEAD
   git push origin v0.12.0
   ```

5. Check the `release` workflow and the GitHub Release. Download an archive for
   your platform and its `.sha256` file. Check and extract it, then confirm the
   version:

   ```sh
   sha256sum -c unobin-v0.12.0-linux-amd64.tar.gz.sha256
   tar -xzf unobin-v0.12.0-linux-amd64.tar.gz
   ./unobin version
   ```

On macOS, use `shasum -a 256 -c` in place of `sha256sum -c`.

The workflow uses the repository's `GITHUB_TOKEN` with write permission only in
the publishing job. No additional release secret is needed.

## Failed releases

If checks or builds fail, the workflow does not publish a GitHub Release. Fix
source or changelog problems in a new commit and use a new version. For a
transient service failure, rerun the workflow for the same commit.

If publishing fails, inspect the GitHub Release for partial uploads before a
retry. Published Go module versions can be cached outside GitHub: do not move or
delete a release tag to replace its contents.
