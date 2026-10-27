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
(`SHRM_BUILD_TOKEN_SHAURMA`, `SHRM_BUILD_TOKEN_BASE`, `CF_API_KEY`, `GROQ_API_KEY`, `GEMINI_API_KEY`,
`SHAURMA_GOOGLE_CLIENT_ID`, `SHAURMA_GOOGLE_CLIENT_SECRET`, `SHAURMA_MANIFEST_PUBKEYS`; see the header of
`scripts/build-windows.ps1`). Without them the build still works and the matching feature is simply disabled.

Releases are produced only by GitHub Actions (`.github/workflows/release.yml`) from a `v*` tag.

## Code signing policy

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
