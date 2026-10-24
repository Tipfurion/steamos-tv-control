# Publishing releases

## One-time repository setting

Before the first release, enable **Settings → General → Releases → Enable
release immutability** on GitHub. This locks published release assets and their
tags. It applies only to future releases. See [GitHub's instructions](https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/establish-provenance-and-integrity/prevent-release-changes).

This is a repository setting, not a tracked workflow option. The workflow
reports a warning if the published release is not immutable. It never replaces
existing release assets or moves tags.

## Cut a version

Commit and push the tested changes, then create and push an annotated tag:

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The tag workflow checks out that commit, runs race-enabled tests, vet,
ShellCheck, the read-only installer and PTY tests, then builds the release.
Tags such as v0.1.0-rc.1 are published as prereleases and do not become latest.

## Release contents

- install.sh, with its release version embedded.
- steamos-tv-control_linux_amd64.tar.gz, built from the same tagged commit.
- SHA256SUMS, covering both the installer and binary archive.

The workflow creates a draft, attaches all assets, and only then publishes it.
Release notes include an installation command pinned to that tag and links to
the corresponding instructions. A failed build does not publish a release.
An upload failure may leave an unpublished draft for inspection.

## Installation behavior

The README uses releases/latest/download/install.sh to choose the latest
published version. Once downloaded, the installer uses its embedded version
for every subsequent asset request. It never resolves latest again. A new
release published during installation cannot mix its files with the old one.

For an exact version:

```bash
curl -fsSL https://github.com/Tipfurion/steamos-tv-control/releases/download/v0.1.0/install.sh | bash
```

The installer rejects a conflicting version override. Source install.sh is a
template for packaging; test fixtures may supply a version and local download
URL explicitly. It is not the public installation entrypoint.

## Local packaging checks

```bash
make release VERSION=v0.1.0
python3 scripts/test-release-package.py
```

Tests check archive contents, checksums, embedded version, pinned URLs, and
rejection of mixed installer/application versions. On Linux amd64 they also
execute the packaged binary and compare its reported version.

Published versions are never overwritten. Fix mistakes with a new patch tag.
The public installation command is usable only after the first release and
its assets have been published.
