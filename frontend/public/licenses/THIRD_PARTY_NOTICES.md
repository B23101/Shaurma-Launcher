# Third-party notices

Shaurma Launcher's own source code is licensed under MIT; the root LICENSE applies.
Third-party software and fonts retain their respective licenses, including copyright notices.
The maintainer confirmed that `skin-model-launcher*.glb` models and `chat-notify.mp3`
were created for this project; they are distributed under the project's MIT license.

## Bundled fonts and frontend libraries

| Component | Origin | License / included notice |
|---|---|---|
| Inter | https://github.com/rsms/inter | SIL OFL 1.1, Inter-OFL.txt |
| JetBrains Mono | https://github.com/JetBrains/JetBrainsMono | SIL OFL 1.1, JetBrainsMono-OFL.txt |
| Tabler Icons webfont | https://github.com/tabler/tabler-icons | MIT, Tabler-MIT.txt |
| Svelte | https://github.com/sveltejs/svelte | MIT, svelte-LICENSE.txt |
| Three.js | https://github.com/mrdoob/three.js | MIT, three-LICENSE.txt |
| Marked | https://github.com/markedjs/marked | MIT, marked-LICENSE.txt |

The font licenses permit bundling with MIT software; the fonts themselves remain under OFL.
Build-time dependencies are pinned by frontend/package-lock.json. Transitive Go dependencies
are pinned by each module's go.mod/go.sum. Generate a dependency license inventory before
release using the procedure in scripts/RELEASING.md; this summary does not replace their licenses.

## Native dependencies and external assets

Wails, Tailscale, SQLite and Go platform libraries retain their upstream license terms.
System components such as WebView2, GTK and WebKitGTK are not relicensed as launcher code.
Java runtimes, Minecraft game files and mod loaders downloaded on demand are separate upstream
software and must not be signed using the launcher's SignPath subscription.

Minecraft character textures and platform/project logos remain subject to their owners'
copyright and trademark terms. Inclusion does not imply endorsement. Maintainers must retain
source and permission evidence for each redistributed asset; no blanket MIT claim is made
for third-party logos, Minecraft assets or downloaded game content.
