# Shaurma Launcher

A Minecraft launcher for Shaurma modpacks and servers: modpack and mod manager, Microsoft/Shaurma
accounts, friends, game console, server tools and an automatic, signed updater.
Windows (Linux builds follow). Official site: <https://shaurma-launcher.org/>

## Download

Get `ShaurmaLauncher-setup.exe` from the [Releases](../../releases) page. Stable releases are marked
*Latest*; beta versions (`X.Y.Z.W`) are published as *pre-releases* and reach only users who chose the beta
channel. Every file can be checked against the build that produced it:

```
gh attestation verify ShaurmaLauncher-setup.exe -R B23101/Shaurma-Launcher
```

## How it is built

| Part | Folder | Output |
|---|---|---|
| Native shell (Wails v3) | `shell/` | `ShaurmLauncher.exe` |
| Backend (C ABI library) | `backend/` | `backend.dll` |
| Updater (C ABI library) | `updater-backend/`, `updater-frontend/` | `updater.dll` |
| Signature contracts (keyset, manifest, tokens) | `updater-backend/trust/` | shared Go module |
| User interface (Svelte) | `frontend/` | `frontend.zip` |
| Windows installer (Inno Setup) | `installer/` | `ShaurmaLauncher-setup.exe` |

Requirements (Windows): Go (see `backend/go.mod`), Node.js 20+, a C compiler for cgo (MSYS2 clang64 or
mingw-w64 gcc), Inno Setup 6.

```
powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1 -Step all -Bundle -Edition shaurma -Version 0.0.0
```

`-Bundle` makes the installer ship every component (nothing is downloaded on first start). Public keys of
the update system come from `SHAURMA_ROOT_PUBKEYS`, `SHAURMA_KEYSET_B64` and `SHAURMA_KEYSET_SIG`.
Private keys and tokens are **never** stored in this repository.

### Local offline build

```
powershell -ExecutionPolicy Bypass -File scripts\build-local-full.ps1
```

Writes `dist\local\ShaurmLauncher-setup.exe` and a portable copy; `local-only.flag` disables the updater.

## Releases

There is one launcher and one release workflow: `.github/workflows/build-launcher.yml`. It runs only when
the maintainer's Pack Manager dispatches it with a build order signed by the release key; there are no tag
triggers. The workflow builds exactly the ordered commit from `main`, takes the version from
`installer/VERSION`, uses no dependency caches and embeds no channel tokens. It attests the final (signed)
files and creates a draft release. Pack Manager then verifies the attestation and hashes, uploads the signed
`launcher-manifest.json`, publishes the release with generated notes and announces the version to
`api.shaurma-launcher.org`. See [scripts/RELEASING.md](scripts/RELEASING.md).

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
- The signed binaries contain no proprietary components. Like other open-source launchers, `backend.dll`
  embeds public client identifiers of third-party APIs (CurseForge API key, Google OAuth desktop sign-in
  client); they identify the app, not a user, and grant no access to user data or project infrastructure.
  No signing keys, tokens of users or server secrets are embedded. Private modpacks are data on a server,
  not code of the launcher.
- Linux archives are not Authenticode-signed; they carry GitHub build-provenance attestations instead.
- Report a suspicious binary through [security reporting](.github/SECURITY.md).

Two independent layers protect updates: Authenticode (for the operating system) and an Ed25519-signed
update manifest (for the launcher). A failure of one does not open the other.

## Privacy policy

This program does not transfer personal information to other networked systems unless you use a feature that needs it
(accounts, friends, cloud playtime, mod downloads, updates). Details: [PRIVACY.md](PRIVACY.md).

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
