# Security policy

## Supported versions
Only the latest release gets security fixes.

## Reporting a vulnerability
Please report it **privately** via [GitHub private vulnerability reporting](https://github.com/Bam679/paperless-cli/security/advisories/new). Don't open a public issue. You can expect an acknowledgement within 7 days.

Especially relevant reports:
- any way to make `paperless` perform a write that the policy file denies (flags, env vars, stdin, argument tricks)
- any way for input to become script code (AppleScript/JXA injection) or a shell command
- secrets or personal data appearing in logs, errors or crash output

## Verifying releases
Release checksums are signed keylessly with Sigstore cosign, and every archive has GitHub build provenance (SLSA):

```sh
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/Bam679/paperless-cli/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
sha256sum --ignore-missing -c checksums.txt
gh attestation verify paperless_*_darwin_arm64.tar.gz --repo Bam679/paperless-cli
```
