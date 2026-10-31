# Pack page designs — protocol v1

The launcher keeps all real buttons, tabs, settings and launch checks in native
Svelte components. A design supplies an HTML scene and can select a native menu layout.
The Mafia layout places the brand, actions and menu on the left, with a separately
scrolling detail panel on the right. It restyles the real interactive controls,
including slider tracks, switches, fields, menus and disabled states. It cannot call the launcher bridge or replace actions.
The user can switch back to the standard design; the choice is saved per pack.

## Pack Manager deployment contract

The private Pack Manager implementation lives in the ignored `pack-manager/`
directory. Its pack editor accepts a design file, and full deployment or cosmetic
publication hashes and publishes it using these optional `packs-index.json` fields:

```json
{
  "pageDesignPath": "packs/mafia/design/<sha256>.html",
  "pageDesignSha256": "<64 lowercase hexadecimal characters>",
  "pageDesignUpdatedAt": "2026-10-31T09:00:00Z"
}
```

1. Read the selected UTF-8 HTML file. All styles and assets must be inline
   (images/fonts can use data URLs). No external resources or scripts are allowed.
2. Compute SHA-256 of the exact published bytes. Compare with the last deployed
   design hash, independently of the pack version. If unchanged, keep the design
   path, hash and timestamp unchanged and do not upload it again.
3. If changed, upload to an immutable hash-based path, then publish the index
   fields with the new hash and UTC timestamp. Publish the file before the index.
4. Alternatively publish a ZIP containing **only** `index.html`; hash the ZIP
   bytes, not its contents. Both archive and expanded HTML are limited to 4 MiB.
   The launcher expands this entry in memory; it never extracts paths to disk.
5. Omit the fields to remove a remote design. The built-in scene remains available
   for the official pack whose exact ID is `mafia`; other IDs use standard UI.

The launcher validates SHA-256 and stores the artifact and update timestamp in
`cache/pack-page-designs/<sha256>.json`. Identical content is reused after a restart;
changed content downloads once. Corrupt entries are downloaded again. The index
still uses the normal authorized catalog refresh. Download errors leave the native
page functional, using the bundled Mafia scene when applicable.

This version uses one self-contained artifact, so updating it replaces that file;
it does not implement per-image differential downloads. A general multi-file
archive/asset manifest would require a new protocol version.

## Palette

Set `<meta name="shaurma-orange" content="#e3bc70">` inside the HTML.
Supported names after `shaurma-`: `bg-panel`, `bg-card`, `bg-input`, `bg-input-hi`,
`text`, `text-mute`, `text-dim`, `orange`, `orange-2`, `border`.
Values must be six-digit hex colors. No arbitrary CSS enters the launcher DOM.
The scene runs in an empty sandbox with a restrictive CSP and no pointer/keyboard
interaction. Include `prefers-reduced-motion` and responsive styles in designs.

## Mafia layout

`<meta name="shaurma-skin" content="mafia">` selects the native Mafia menu layout
implemented in `mafia.css`. The HTML is not a decorative banner in this mode.
An `<img data-shaurma-background src="data:image/png;base64,...">` supplies the
full-screen background; without it the pack's catalog background is used.
The image pans with a 40-second return cycle and 25% overscan. All animated
movement respects reduced motion. The standard-design toggle restores the normal
launcher layout, including the user's full-banner preference.

Pack Manager's **Меню Mafia з мода** action asks for the mod source directory,
reads its `src/main/resources/assets/mafia_mod/textures/gui/main_menu/background.png`,
and creates a self-contained local HTML artifact. Save the pack and publish its
cosmetics or deploy it. A ready private artifact also lives at
`pack-manager/designs/mafia.html` in this workspace.

Public template and CSS are MIT-licensed. The mod's original background is kept
only in the ignored private deployment artifact, not in public source assets.
The launcher uses its existing bundled monospace font for native controls.
