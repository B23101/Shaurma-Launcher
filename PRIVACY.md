# Privacy policy

Shaurma Launcher is open source. This page lists what the program sends over the network and why.
It does not contain advertising and does not sell data.

## Always (needed for the launcher to work)

- **Game files and metadata:** Mojang/Microsoft (versions, libraries, assets, skins), mod-loader metadata and downloads
  (Forge, NeoForge, Fabric, Quilt, Paper, Purpur and others), Modrinth and CurseForge (mod/modpack search and downloads).
- **Updates:** the launcher asks `api.shaurma-launcher.org` which version is current (it sends the release channel and the
  installed version; the updater also sends the platform). If that server is silent, the launcher reads the latest release
  manifest from this repository on GitHub. Components are downloaded from this repository's GitHub Releases and verified
  against an Ed25519-signed manifest. If the updater rejects an update, it sends a short report (version and reason, no
  personal data). Older builds still fetch updates from the Shaurma CDN with a channel access token.
- **Access to private modpacks (only if you add a certificate):** activation sends the certificate file, a public device key
  generated on your computer, the launcher version and operating system, and optionally your nickname. The server stores the
  device record (key, time, IP address, launcher version) to grant, log and revoke access; inactive devices expire after
  180 days. The private part of the device key never leaves your computer (Windows: protected with DPAPI). "Unlink this
  device" in Settings → Access deletes the key locally and revokes it on the server.
- **Microsoft sign-in:** handled by Microsoft/Xbox services in your browser; the launcher stores the resulting tokens locally.

## Only if you use the feature

- **Friends, chat:** your account identifiers, nickname, friend list and messages are exchanged with Shaurma servers.
- **Cloud playtime (on by default once you are signed in to a Shaurma account):** the launcher sends your total play time per
  day (seconds and session count, for the last 14–80 days), older history once as monthly totals, and the number of packs
  that have playtime. Pack names, mod lists and the per-pack breakdown are not sent; they stay on your computer. You can
  see your own data; friends who can open your profile see only the total time and the pack count. Turn it off in
  Settings ("Don't store play time in the cloud") or stop recording time altogether ("Record time in packs").
- **Peer-to-peer play and server tools:** connections are relayed or brokered by Shaurma servers. A friend can start or stop
  your servers remotely **only** if you explicitly grant that permission; nothing is allowed by default and every attempt is
  logged locally.
- **AI helpers (translation, review, crash analysis):** the text you submit is sent to the AI provider (Groq / Google Gemini).
  For crash analysis the launcher first removes tokens, session ids, launch arguments, your nickname, OS user name,
  computer name and server addresses from the log.
- **Log sharing:** if you choose to upload a game log, it is sent to mclo.gs.
- **Discord Rich Presence:** talks to your local Discord client only.

## No usage statistics

The launcher does not send usage analytics or telemetry. The "Analytics" page in the launcher is computed from local
playtime records on your computer; the only playtime that leaves it is the optional cloud playtime described above.

## Local data

Settings, accounts and modpacks are stored on your computer (default `%APPDATA%\.shaurm`). Uninstalling keeps them unless you
choose to delete them.

## Contact

Open an issue in this repository.

## Providers' privacy policies

The services you choose also process connection metadata such as IP addresses according to their own policies:

- Microsoft/Mojang/Xbox sign-in and game services: https://privacy.microsoft.com/privacystatement
- Google sign-in and Gemini (when used): https://policies.google.com/privacy
- Groq (when used): https://groq.com/privacy-policy/
- Modrinth: https://modrinth.com/legal/privacy
- CurseForge: https://www.curseforge.com/legal/privacy-policy
- Discord (when Rich Presence is enabled): https://discord.com/privacy

Release builds made by the project's CI embed Gemini and Groq API keys taken from GitHub Secrets, and may embed a
CurseForge key. Any key shipped in a client binary must be treated as publicly recoverable; users may replace the CurseForge
key with their own in Settings. No credential in a client binary can serve as proof of membership or permission to access private data.

## Network permissions and optional features

The Windows installer offers a P2P permission option, selected by default, which adds
application-specific inbound and outbound firewall rules for private and public networks.
It requires administrator approval. The firewall is not disabled. Uninstallation removes
the rules belonging to this installation. Existing block rules and organization policies
may still prevent P2P access. P2P, remote server access, AI helpers and social features are
activated by using those features.

After a P2P connection failure, or when you select the network check in Settings,
the launcher can inspect its Windows Firewall rules and contact the P2P control
server's health endpoint. Missing allow rules do not by themselves mean a connection
is blocked. The optional repair button requests Windows administrator approval and
configures only the launcher's own inbound/outbound rules for private and public
networks. It does not disable protection, remove block rules, or override organization
policies. Declining approval leaves the rules unchanged. Active sessions are not
restarted by this repair. The helper runs directly in the launcher EXE without scripts.

Account deletion and server-side retention must be confirmed with the operator of Shaurma
services; uninstalling removes local application files, not remote account data. Never attach
account databases or unredacted logs to public issues. Security reporting instructions:
https://github.com/B23101/Shaurma-Launcher/blob/main/.github/SECURITY.md

## Shared account and skin service

The launcher supports a Shaurma identity for skin management and Google/Microsoft sign-in.
The identity service receives the provider identity token needed to verify sign-in and account
identifiers used for the skin account. Friends, chat, P2P sharing, remote server access and
cloud playtime follow the same account and permission rules for everyone. The private modpack
catalog is shown only on devices activated with an access certificate (see above).

When the Shaurma skin service is enabled, the launcher attaches authlib-injector to Minecraft
for all account types, including Microsoft accounts. The local proxy sends game session
authorization (`join`, `hasJoined`) and Minecraft services requests directly to Mojang/Microsoft,
without forwarding game access tokens to Shaurma. Authentication redirects are refused.
Shaurma receives skin/profile lookups, not arbitrary session requests. If the local proxy
cannot start, licensed accounts launch without the injector using official services.
Only offline accounts with no credentials (empty access token or the offline placeholder `0`)
may fall back directly to Shaurma skins. Disabling Mojang skin fallback does not disable the
local authentication proxy. This is separate from browser-based Microsoft sign-in and the
identity token used to create or recover the Shaurma account.
To avoid this routing, deselect the skin-service option during Windows installation or turn
off the Shaurma skin service in Settings. This prevents agent attachment and automatic
Shaurma skin previews. Explicit account-management actions still contact the chosen service.

The downloaded authlib-injector v1.2.8 is separate upstream software (AGPL-3.0), not signed
under this project's certificate. Its SHA-256 is pinned to the digest published with the
official GitHub release and checked for cached and newly downloaded files.
