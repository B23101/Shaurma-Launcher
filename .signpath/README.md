# Signing configuration

Templates for the SignPath project `shaurma-launcher` (SignPath Foundation). Their presence does not
activate the service or confirm that the application was approved.

| Configuration slug | File | Signs |
|---|---|---|
| `components` | `artifact-configuration.xml` | `ShaurmLauncher.exe`, `backend.dll`, `updater.dll` |
| `installer` | `artifact-configuration/installer.xml` | `ShaurmLauncher-setup.exe` |

There is one launcher and one build origin: `.github/workflows/build-launcher.yml` on GitHub-hosted runners,
started only by a signed build order for a commit on `main`. No dependency caches are used in the release
jobs; the version comes from `installer/VERSION` in the commit. The binaries contain no proprietary
components and no channel tokens: private modpacks are data on a server, not code of the launcher.

All configurations restrict product name to `Shaurma Launcher` and product version to `1.0.0`, matching
the PE metadata contract (`scripts/build-windows.ps1`, `scripts/verify-signatures.ps1`). The application
release version is separate and lives in `installer/VERSION` and the signed update manifest.

## Setup in SignPath

1. GitHub as trusted build system; allowed origin: this repository, workflow `build-launcher.yml`, branch `main`.
2. Artifact configurations `components` and `installer` from the files above.
3. Signing policies: `release-signing` (manual approval by an Approver for every request, origin verification
   on, branch `main`) and `test-signing` (for checks only; not a publicly trusted certificate).
4. MFA for everyone with access to the repository or to SignPath.
5. GitHub secret `SIGNPATH_API_TOKEN` in environment `release` (`scripts/set-github-secret.ps1`) and
   repository variable `SIGNPATH_ORGANIZATION_ID`.

Only project-owned binaries are signed. Downloaded Java, Minecraft, mod-loader or other upstream binaries are
never submitted. Until approval, releases go out with `authenticode: waived` in the signed update manifest:
update security does not depend on Authenticode.

Source policy: https://signpath.org/terms · Artifact syntax: https://docs.signpath.io/artifact-configuration/syntax