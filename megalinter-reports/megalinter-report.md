## ✅[MegaLinter](https://megalinter.io/10.1.0) analysis: Success



| Descriptor  |                                                Linter                                                |Files|Fixed|Errors|Max errors|Warnings|Elapsed time|
|-------------|------------------------------------------------------------------------------------------------------|----:|----:|-----:|---------:|-------:|-----------:|
|✅ COPYPASTE |[jscpd](https://megalinter.io/10.1.0/descriptors/copypaste_jscpd)                                     |  yes|     |    no|          |      no|       1.02s|
|✅ CSS       |[stylelint](https://megalinter.io/10.1.0/descriptors/css_stylelint)                                   |    1|    0|     0|          |       0|       2.05s|
|✅ GO        |[golangci-lint](https://megalinter.io/10.1.0/descriptors/go_golangci_lint)                            |  yes|  yes|    no|          |      no|      98.46s|
|✅ GO        |[revive](https://megalinter.io/10.1.0/descriptors/go_revive)                                          |   30|     |     0|          |       0|       0.52s|
|✅ HTML      |[djlint](https://megalinter.io/10.1.0/descriptors/html_djlint)                                        |    4|     |     0|          |       0|       8.21s|
|✅ HTML      |[htmlhint](https://megalinter.io/10.1.0/descriptors/html_htmlhint)                                    |    4|     |     0|          |       0|       1.19s|
|✅ JSON      |[jsonlint](https://megalinter.io/10.1.0/descriptors/json_jsonlint)                                    |    1|     |     0|          |       0|       0.25s|
|✅ JSON      |[prettier](https://megalinter.io/10.1.0/descriptors/json_prettier)                                    |    1|    0|     0|          |       0|       2.36s|
|✅ JSON      |[v8r](https://megalinter.io/10.1.0/descriptors/json_v8r)                                              |    1|     |     0|          |       0|       3.95s|
|✅ MARKDOWN  |[markdownlint](https://megalinter.io/10.1.0/descriptors/markdown_markdownlint)                        |    4|    0|     0|          |       0|       2.33s|
|✅ MARKDOWN  |[markdown-table-formatter](https://megalinter.io/10.1.0/descriptors/markdown_markdown_table_formatter)|    4|    0|     0|          |       0|       0.94s|
|✅ REPOSITORY|[betterleaks](https://megalinter.io/10.1.0/descriptors/repository_betterleaks)                        |  yes|     |    no|          |      no|       1.94s|
|✅ REPOSITORY|[checkov](https://megalinter.io/10.1.0/descriptors/repository_checkov)                                |  yes|     |    no|          |      no|      47.72s|
|✅ REPOSITORY|[git_diff](https://megalinter.io/10.1.0/descriptors/repository_git_diff)                              |  yes|     |    no|          |      no|       0.63s|
|✅ REPOSITORY|[grype](https://megalinter.io/10.1.0/descriptors/repository_grype)                                    |  yes|     |    no|          |      no|     174.21s|
|✅ REPOSITORY|[osv-scanner](https://megalinter.io/10.1.0/descriptors/repository_osv_scanner)                        |  yes|     |    no|          |      no|       0.41s|
|✅ REPOSITORY|[secretlint](https://megalinter.io/10.1.0/descriptors/repository_secretlint)                          |  yes|     |    no|          |      no|       1.37s|
|✅ REPOSITORY|[syft](https://megalinter.io/10.1.0/descriptors/repository_syft)                                      |  yes|     |    no|          |      no|       8.85s|
|✅ REPOSITORY|[trivy](https://megalinter.io/10.1.0/descriptors/repository_trivy)                                    |  yes|     |    no|          |      no|      17.56s|
|✅ REPOSITORY|[trivy-sbom](https://megalinter.io/10.1.0/descriptors/repository_trivy_sbom)                          |  yes|     |    no|          |      no|        0.5s|
|✅ REPOSITORY|[trufflehog](https://megalinter.io/10.1.0/descriptors/repository_trufflehog)                          |  yes|     |    no|          |      no|       4.93s|
|✅ SPELL     |[cspell](https://megalinter.io/10.1.0/descriptors/spell_cspell)                                       |   42|     |     0|          |       0|       6.95s|
|✅ SPELL     |[lychee](https://megalinter.io/10.1.0/descriptors/spell_lychee)                                       |    9|     |     0|          |       0|      83.86s|

See detailed reports in MegaLinter artifacts


Your project could benefit from a custom flavor, which would allow you to run only the linters you need, and thus improve runtime performances. (Skip this info by defining `FLAVOR_SUGGESTIONS: false`)

  - Documentation: [Custom Flavors](https://megalinter.io/10.1.0/custom-flavors/)
  - Command: `npx mega-linter-runner@10.1.0 --custom-flavor-setup --custom-flavor-linters COPYPASTE_JSCPD,CSS_STYLELINT,GO_GOLANGCI_LINT,GO_REVIVE,HTML_DJLINT,HTML_HTMLHINT,JSON_JSONLINT,JSON_V8R,JSON_PRETTIER,MARKDOWN_MARKDOWNLINT,MARKDOWN_MARKDOWN_TABLE_FORMATTER,REPOSITORY_CHECKOV,REPOSITORY_GIT_DIFF,REPOSITORY_BETTERLEAKS,REPOSITORY_GRYPE,REPOSITORY_OSV_SCANNER,REPOSITORY_SECRETLINT,REPOSITORY_SYFT,REPOSITORY_TRIVY,REPOSITORY_TRIVY_SBOM,REPOSITORY_TRUFFLEHOG,SPELL_CSPELL,SPELL_LYCHEE`

[![MegaLinter is provided by OX Security](https://raw.githubusercontent.com/oxsecurity/megalinter/main/docs/assets/images/ox-banner.png)](https://www.ox.security/?ref=megalinter)
Show us your support by [**starring ⭐ the repository**](https://github.com/oxsecurity/megalinter)