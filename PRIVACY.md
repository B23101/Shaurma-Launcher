# Privacy policy

Shaurma Launcher is open source. This page lists what the program sends over the network and why.
It does not contain advertising and does not sell data.

## Always (needed for the launcher to work)

- **Game files and metadata:** Mojang/Microsoft (versions, libraries, assets, skins), mod-loader metadata and downloads
  (Forge, NeoForge, Fabric, Quilt, Paper, Purpur and others), Modrinth and CurseForge (mod/modpack search and downloads).
- **Updates:** the launcher downloads its own components from the Shaurma CDN and verifies them (SHA-256, and a signed manifest
  when the build contains signing keys). Requests carry the release channel and a channel access token.
- **Microsoft sign-in:** handled by Microsoft/Xbox services in your browser; the launcher stores the resulting tokens locally.

## Only if you use the feature

- **Friends, chat, shared playtime (full Shaurma edition):** your account identifiers, nickname, friend list,
  messages and total playtime per day are exchanged with Shaurma servers. Playtime synced to the cloud is visible only to you.
- **Peer-to-peer play and server tools:** connections are relayed or brokered by Shaurma servers. A friend can start or stop
  your servers remotely **only** if you explicitly grant that permission; nothing is allowed by default and every attempt is
  logged locally.
- **AI helpers (translation, review, crash analysis):** the text you submit is sent to the AI provider (Groq / Google Gemini).
- **Log sharing:** if you choose to upload a game log, it is sent to the log-sharing service you pick.
- **Discord Rich Presence:** talks to your local Discord client only.

## Anonymous statistics (opt-in, OFF by default)

If you accept the one-time question in the launcher, it sends a small report at most every few hours: nickname (encrypted on
arrival), anonymous device hash, total playtime and session count, launcher version/edition and platform.
It never sends tokens, e-mail, file paths, modpack names or mod lists. You can turn it off in Settings at any time.

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

AI provider keys are not embedded by the repository release scripts. AI features require
user-supplied credentials at runtime unless a separately operated service is explicitly configured.
A CurseForge key supplied with a distributed desktop build must be treated as publicly recoverable;
users may override it with their own key in Settings. No credential in a client binary can serve as
proof of membership or permission to access private data.

## Network permissions and optional features

The Windows installer offers a P2P permission option, selected by default, which adds
application-specific inbound and outbound firewall rules for private and public networks.
It requires administrator approval. The firewall is not disabled. Uninstallation removes
the rules belonging to this installation. Existing block rules and organization policies
may still prevent P2P access. P2P, remote server access, AI helpers and social features are
activated by using those features; anonymous statistics remain off until you opt in.

Account deletion and server-side retention must be confirmed with the operator of Shaurma
services; uninstalling removes local application files, not remote account data. Never attach
account databases or unredacted logs to public issues. Security reporting instructions:
https://github.com/B23101/Shaurma-Launcher/blob/main/.github/SECURITY.md

## Shared account and skin service (Base and full editions)

Both editions support a Shaurma identity for skin management and Google/Microsoft sign-in.
The identity service receives the provider identity token needed to verify sign-in and account
identifiers used for the skin account. Friends, chat, P2P sharing, remote server access and
cloud playtime are available in both editions under the same account and permission rules.
Base excludes only the private modpack catalog and its download client; it does not receive
the private pack token.

When the Shaurma skin service is enabled, the launcher attaches authlib-injector to Minecraft
for all account types, including Microsoft accounts. Skin/profile and game session requests
can pass through the local skin proxy and Shaurma's Yggdrasil service; session requests may
contain game session authorization data. This is separate from browser-based Microsoft sign-in.
To avoid this routing, deselect the skin-service option during Windows installation or turn
off the Shaurma skin service in Settings. This prevents agent attachment and automatic
Shaurma skin previews. Explicit account-management actions still contact the chosen service.

The downloaded authlib-injector v1.2.8 is separate upstream software (AGPL-3.0), not signed
under this project's certificate. Its SHA-256 is pinned to the digest published with the
official GitHub release and checked for cached and newly downloaded files.
