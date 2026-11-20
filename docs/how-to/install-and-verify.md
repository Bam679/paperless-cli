# How to install and verify a paperless release

This guide shows you how to install `paperless` without Homebrew and check that the binary was built by this repository's release workflow. Homebrew users can skip it: `brew install Bam679/tap/paperless` installs the same archives, and the tap only accepts releases whose signature checks out.

You need [cosign](https://github.com/sigstore/cosign) and the GitHub CLI (`gh`) for the checks.

## Download and verify an archive

Set the version, operating system and architecture you want (`darwin`, `linux` or `windows` with `arm64` or `amd64`; Windows archives are `.zip`):

```sh
V=0.1.0 OS=darwin ARCH=arm64
base=https://github.com/Bam679/paperless-cli/releases/download/v$V
curl -fsSLO "$base/paperless_${V}_${OS}_${ARCH}.tar.gz"
curl -fsSLO "$base/checksums.txt"
curl -fsSLO "$base/checksums.txt.sigstore.json"
```

Check that `checksums.txt` was signed by the release workflow of this repository:

```sh
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/Bam679/paperless-cli/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
```

The last line of the output is `Verified OK`. Then check the archive against the signed checksums, and check its build provenance:

```sh
shasum -a 256 --ignore-missing -c checksums.txt
gh attestation verify "paperless_${V}_${OS}_${ARCH}.tar.gz" --repo Bam679/paperless-cli
```

If any of the three checks fails, don't install the binary, and please [report it](../../SECURITY.md).

## Install the binary

```sh
tar xzf "paperless_${V}_${OS}_${ARCH}.tar.gz" paperless
install -m 755 paperless ~/.local/bin/
paperless version
```

Make sure `~/.local/bin` is on your `PATH`. Every archive also has an SBOM (`*.sbom.json`) that lists what went into the build.

## Build from source instead

```sh
go install github.com/Bam679/paperless-cli/cmd/paperless@latest
```

This needs Go 1.26 or later. A source build reports its module version, but it has no signature or provenance of its own.
