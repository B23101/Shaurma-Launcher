# Shaurma Launcher

A Minecraft launcher for Shaurma modpacks and servers: modpack and mod manager (Modrinth, CurseForge),
Microsoft/Shaurma accounts, friends, game console, server tools and an automatic updater.
Windows and Linux. Official site: <https://shaurma-launcher.org/>

## Download

Get installers from the [Releases](../../releases) page or the official site.
Windows installers are code-signed (see below). Linux archives carry a GitHub build attestation:

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

Tokens and API keys are **never** stored in this repository. The build reads them from environment variables
(`SHRM_BUILD_TOKEN_SHAURMA`, `SHRM_BUILD_TOKEN_BASE`, `CF_API_KEY`, Desktop OAuth client settings,
`SHAURMA_MANIFEST_PUBKEYS`; see `scripts/build-windows.ps1` and `scripts/RELEASING.md`).
Release builds require their edition's CDN token and manifest public keys. AI keys are supplied
by the user at runtime and are not embedded by these release scripts.

Releases are produced only by GitHub Actions (`.github/workflows/release.yml`) from a `v*` tag.

## Code signing policy

Planned signing integration (subject to SignPath approval and configuration):

Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org).

- Only binaries built by the GitHub Actions workflow in this repository, from the source code in this repository,
  are submitted for signing. Build scripts and CI configuration are part of the signed source.
- Every release is approved manually before it is signed.
- Team roles
  - Committers and reviewers: [B23101](https://github.com/B23101)
  - Approvers: [B23101](https://github.com/B23101)
- Linux archives are not Authenticode-signed; they carry GitHub build-provenance attestations instead.

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

The public workflow builds Base only. Full Shaurma builds must run in an approved private
repository and remain private; both editions have separate updater binaries and signing
configurations. Service-side access must use user authorization, not possession of an embedded token.
Shaurma account, social and update services are operated separately from this open-source client.

See [release preparation and remaining account settings](scripts/RELEASING.md),
[security reporting](.github/SECURITY.md), [contributing](.github/CONTRIBUTING.md), and
[third-party notices](frontend/public/licenses/THIRD_PARTY_NOTICES.md).

Base includes all launcher features: accounts, skins, friends, chat, P2P, sharing and remote
server access. Only the private modpack catalog and its download client require the full edition.
The optional skin service can route game session requests through Shaurma for all account types.
Review the privacy policy; it can be disabled during installation or in Settings.
