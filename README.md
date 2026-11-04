# Shaurma Launcher

A Minecraft launcher for Shaurma modpacks and servers: modpack and mod manager (Modrinth, CurseForge),
Microsoft/Shaurma accounts, friends, game console, server tools and an automatic updater.
Windows and Linux. Official site: <https://shaurma-launcher.org/>

## Download

Get installers from the [Releases](../../releases) page or the official site.
Signing is planned and requires SignPath approval. Check the signature of the actual download;
unsigned dry runs are not signed releases. Linux release archives carry a GitHub build attestation:

```
gh attestation verify shaurma-launcher-linux-shaurma.tar.gz -R B23101/Shaurma-Launcher
```

## How it is built

| Part | Folder | Output |
|---|---|---|
| Native shell (Wails v3) | `shell/` | `ShaurmLauncher.exe` / `ShaurmLauncherBase.exe` |
| One-time bootstrap | `bootstrap/` | first-run downloader embedded in the installer |
| Backend (C ABI library) | `backend/` | `backend.dll` / `backend.so` |
| Updater (C ABI library) | `updater-backend/`, `updater-frontend/` | `updater.dll` / `updater.so` |
| User interface (Svelte) | `frontend/` | `frontend.zip` |
| Windows installer (Inno Setup) | `installer/` | `*-setup.exe` |

Requirements (Windows): Go (see `backend/go.mod`), Node.js 20+, a C compiler for cgo (MSYS2 clang64 or mingw-w64 gcc),
Inno Setup 6.

```
scripts\build-windows.bat                      # everything, unsigned
scripts\build-windows.bat -Step components     # only the binaries
bash scripts/build-linux.sh                    # Linux (needs libgtk-4-dev, libwebkitgtk-6.0-dev)
```

### Local full build with an offline installer

```
powershell -ExecutionPolicy Bypass -File scripts\build-local-full.ps1
```

Builds only the full (shaurma) edition and writes `dist\local\ShaurmLauncher-setup.exe` plus a
portable `dist\app-shaurma-local\`. The installer ships shell, backend, updater, both frontend
archives and `launcher-version.json` from local files; `local-only.flag` disables the updater so
nothing is downloaded from the CDN. The Shaurma token is embedded only in `backend.dll` (needed to
download Shaurma packs) and never in `shell.exe` or `updater.dll`. Needs Inno Setup 6; the Base
edition is not built by this script. If Go is missing or older than `backend/go.mod` requires, the
script downloads the official toolchain into `%LOCALAPPDATA%\ShaurmaLauncherBuild` (no administrator
rights).
```

Tokens and API keys are **never** stored in this repository. The build reads them from environment variables
(`SHRM_BUILD_TOKEN_SHAURMA`, `SHRM_BUILD_TOKEN_BASE`, `CF_API_KEY`, Desktop OAuth client settings,
`SHAURMA_MANIFEST_PUBKEYS`; see `scripts/build-windows.ps1` and `scripts/RELEASING.md`).
Release builds require their edition's CDN token and manifest public keys. AI keys are supplied
by the user at runtime and are not embedded by these release scripts.

Releases are produced only by GitHub Actions (`.github/workflows/release.yml`) from a `v*` tag.
There is one launcher and one release workflow: `.github/workflows/build-launcher.yml`. It runs only
when the maintainer's Pack Manager dispatches it with a build order signed by the release key; there
are no tag triggers. The workflow builds exactly the ordered commit from `main`, takes the version
from `installer/VERSION`, uses no dependency caches and embeds no channel tokens. It attests the
final (signed) files and creates a draft release. The release becomes official only after Pack
Manager verifies the attestation and hashes and uploads the signed `launcher-manifest.json`.

## Code signing policy

Planned signing integration (subject to SignPath approval and configuration):

Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org).

- Only binaries built by the GitHub Actions workflow in this repository, from the source code in this repository,
  are submitted for signing. Build scripts and CI configuration are part of the signed source.
- Every release is approved manually before it is signed.
- Team roles
  - Authors and reviewers: [B23101](https://github.com/B23101)
  - Approvers: [B23101](https://github.com/B23101)
- Signed files: `ShaurmLauncher.exe`, `backend.dll`, `updater.dll` and `ShaurmaLauncher-setup.exe` built by
  `build-launcher.yml` from a commit on `main`. Nothing built outside this repository is signed.
- Multi-factor authentication is required for everyone with access to this repository and to SignPath.
- The signed binaries contain no proprietary components and no credentials; private modpacks are data on
  a server, not code of the launcher.
- Linux archives are not Authenticode-signed; they carry GitHub build-provenance attestations instead.
- Report a suspicious binary through [security reporting](.github/SECURITY.md).

Two independent layers protect updates: Authenticode (for the operating system) and an Ed25519-signed
update manifest (for the launcher). A failure of one does not open the other.

## Privacy policy

This program does not transfer personal information to other networked systems unless you use a feature that needs it
(accounts, friends, mod downloads, updates) or you opt in to anonymous statistics. Details: [PRIVACY.md](PRIVACY.md).

## License

[MIT](LICENSE). Minecraft is a trademark of Mojang AB; this project is not affiliated with Mojang or Microsoft.
Mod-loader and platform names/logos belong to their respective owners.

## Network features and release preparation

P2P is optional. The installer offers inbound/outbound permissions for the launcher EXE on
private and public Windows networks, with administrator approval. Remote server control
requires the owner's explicit permission. Existing firewall blocks and organization policies
can still take precedence. No global firewall or antivirus protection is disabled.

There are no editions any more. Access to private modpacks is granted by the server: you add the
shared certificate file once (next to `setup.exe` or in Settings → Access), the launcher creates a
personal device key and receives short-lived access tokens. Without activation the section is not
shown; the launcher is otherwise fully functional and receives all updates.
Shaurma account, social and update services are operated separately from this open-source client.

See [release preparation and remaining account settings](scripts/RELEASING.md),
[security reporting](.github/SECURITY.md), [contributing](.github/CONTRIBUTING.md), and
[third-party notices](frontend/public/licenses/THIRD_PARTY_NOTICES.md).

Base includes all launcher features: accounts, skins, friends, chat, P2P, sharing and remote
server access. Only the private modpack catalog and its download client require the full edition.
The optional skin service can route game session requests through Shaurma for all account types.
Review the privacy policy; it can be disabled during installation or in Settings.

## Component downloads and verification

Updates come only from this repository's GitHub Releases. The launcher asks
`api.shaurma-launcher.org` which version is current (the answer contains a version name and a
rollout pace, never an address); if the server is silent it asks GitHub directly. It then downloads
`launcher-manifest.json` and its `.sig` from the release and accepts it only if:

- it is signed by a release key listed in `keyset.json`, which is signed by the offline root key
  embedded in the launcher (keys rotate without reinstalling);
- its version is newer than the installed one and than any version seen before (no rollback), it has
  not expired, and it belongs to your channel;
- every file matches the size and SHA-256 in the manifest and, unless the manifest says otherwise,
  carries a valid Authenticode signature from a publisher listed in the keyset.

Download addresses are built by the launcher itself from the repository name and the tag; redirects
are allowed only to GitHub's asset hosts. Updates are applied in two phases: if the new version does
not start three times in a row, the previous files are restored automatically.

Verify a file yourself: `gh attestation verify <file> -R B23101/Shaurma-Launcher`.
The commit, build run and SHA-256 of every file are listed in the release and in the manifest.
