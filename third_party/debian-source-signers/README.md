# Frozen Debian source-signature certificates

The five unmodified public certificate files come from `debian-keyring-pgp/`
in the official [Debian keyring repository](https://salsa.debian.org/debian-keyring/keyring)
at revision `83d66888437c84e460765fc4f88d5cd7ceedfbc4`.
The original repository license is retained as `LICENSE`.

`server/cmd/identity-release/auth.go` pins each certificate's bytes and its
permitted signing/primary fingerprint, paired in the frozen descriptor order:
base-files, netbase, tzdata, media-types and ca-certificates. This is a narrow
historical cryptographic-match policy. GPGV treats supplied certificates as
trusted and ignores expiration/revocation dates; success is not personal owner
trust, current certificate validity, or Debian archive Release verification.
The media-types certificate's expired status is retained rather than presented
as current validity. No private key or customer identity is present here.
