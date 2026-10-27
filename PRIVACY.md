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

- **Shaurma account, friends, chat, shared playtime (Shaurma edition):** your account identifiers, nickname, friend list,
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
