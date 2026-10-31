# Signing configuration

These files are templates to install in the SignPath project `shaurma-launcher`.
Their presence does not activate the service or confirm that an application was approved.

| Configuration slug | File | Build visibility |
|---|---|---|
| components | artifact-configuration/components.xml | public Base |
| installer | artifact-configuration/installer.xml | public Base |
| components-team | artifact-configuration/components-team.xml | private Shaurma |
| installer-team | artifact-configuration/installer-team.xml | private Shaurma |

All configurations restrict product name to `Shaurma Launcher` and product version to
`1.0.0`, matching the existing PE metadata contract. The application release version is
separate. Changing this contract requires updating the build script, signature verifier
and approved SignPath configurations together; a fixed metadata version does not guarantee
SmartScreen reputation or approval.

Configure GitHub as a trusted build system. Permit only the approved repository/workflow
origins. Include both editions and both build origins in one application. Approval must
explicitly cover the full-edition private origin as well as public Base. If Foundation cannot accept that origin,
full-edition signing remains blocked until an approved signing arrangement is available.

Use `release-signing` with a required human approver for every signing request; never configure
automatic approval. `test-signing` is for testing and is not a public trusted release certificate.
Only project-owned binaries are signed. Do not submit downloaded Java, Minecraft, mod-loader
or other upstream binaries under this project certificate.

Source policy: https://signpath.org/terms
Artifact syntax: https://docs.signpath.io/artifact-configuration/syntax
Metadata restrictions: https://docs.signpath.io/artifact-configuration/reference
