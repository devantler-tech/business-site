# Go distribution notice fixtures

These verbatim Go 1.26.10 distribution files exercise checked retention without
depending on the test host's installed compiler path. LICENSE has SHA-256
`911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad`;
PATENTS has SHA-256
`96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc`.
They were read from the available Go 1.26.10 distribution. Preserve their bytes,
including line endings and final newline. The upstream project is
[Go](https://go.dev/); these fixtures are not compiler binaries or proof that an
identity distribution's complete notice obligations have been assessed.

Tests create an explicitly synthetic VERSION timestamp. Production captures
LICENSE, PATENTS and VERSION from the actual compiler selected by the Linux
recipe, not from these fixtures. Relevant nested/incorporated and dependency
materials remain separate work under business-site #326.
