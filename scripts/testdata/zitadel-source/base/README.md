# Frozen base-image metadata

`config.json.raw` is the original public Linux/amd64 config blob
`sha256:55a757dc8ab862a4eb634c434261dcd26f17c186b54f7bce845404a335456110`
from `gcr.io/distroless/static-debian13`.
The frozen nonroot index is
`sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3`;
its amd64 manifest is
`sha256:2293b36c7c9082bf4115aab724b4d2cddec82c8eba39bf27ac0517e159acf150`.

The test authenticates the complete config bytes before using its thirteen
RootFS DiffIDs as an independent expected base prefix. This is original metadata,
not a generated application fixture or an own-code license grant. Its primary
registry objects were independently read on 2026-11-01; the index's normal
Cosign verification succeeded for `keyless@distroless.iam.gserviceaccount.com`
with issuer `https://accounts.google.com`. This fixture alone is not a fresh
signature verification or full distribution admission.
