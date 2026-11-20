# Security policy

## Supported versions

Security fixes go into the latest release only.

## Reporting a vulnerability

Please report vulnerabilities privately through [GitHub private vulnerability reporting](https://github.com/Bam679/paperless-cli/security/advisories/new), not in a public issue. You will get an answer within seven days.

These reports are especially welcome:

- a way to make `paperless` perform a write that the policy file denies, through flags, environment variables, stdin or unusual arguments
- a way for input to become script code (AppleScript or JXA injection) or a shell command
- secrets or personal data showing up in error messages or other output where they don't belong

## Verifying a release

The checksum file of every release is signed with Sigstore cosign (keyless, from GitHub Actions), and every archive has GitHub build provenance:

```sh
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/Bam679/paperless-cli/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
shasum -a 256 --ignore-missing -c checksums.txt
gh attestation verify paperless_*_darwin_arm64.tar.gz --repo Bam679/paperless-cli
```
