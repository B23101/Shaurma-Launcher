package com.shaurma.packmanager.core;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.util.*;
import java.util.zip.ZipEntry;
import java.util.zip.ZipInputStream;

/**
 * Розпаковує .mrpack ЛОКАЛЬНО (у тимчасову теку) один раз за деплой і дає
 * структурований доступ до вмісту, потрібний новій deploy-логіці V2
 * (DOWNLOAD_SYNC_DESIGN_V2.md, розділ 3):
 *
 *  - modrinth.index.json -> hashMods (downloads[] непорожній) /
 *                            bundled-моди-кандидати (без downloads[])
 *  - overrides/ (і client-overrides/), крім mods/:
 *      top-level ФАЙЛИ  -> override-file
 *      top-level ТЕКИ    -> override-folder (архівується FolderArchiver'ом)
 *  - overrides/mods/ (і client-overrides/mods/) -> bundled-моди (окремі jar)
 *
 * .mrpack більше НІКОЛИ не публікується як файл на R2 — тільки локальна
 * розпаковка для потреб деплою.
 */
public final class MrpackExtractor {

    private static final Logger log = LoggerFactory.getLogger(MrpackExtractor.class);

    private MrpackExtractor() {}

    /** Один top-level override-файл (не в mods/, не в жодній теці). */
    public record OverrideFile(String localPath, Path absolutePath) {}

    /** Одна top-level override-тека (крім mods/) — кандидат на zip-архів. */
    public record OverrideFolder(String localName, Path absolutePath) {}

    /** Один bundled-мод (jar з overrides/mods/ або client-overrides/mods/). */
    public record BundledModFile(String fileName, Path absolutePath) {}

    public record ExtractionResult(
        Path extractedRoot,
        List<String> mrpackFilePaths,          // "files[]" з modrinth.index.json (сирі шляхи)
        List<OverrideFile> overrideFiles,
        List<OverrideFolder> overrideFolders,
        List<BundledModFile> bundledMods
    ) {}

    /**
     * Розпакувати .mrpack повністю в тимчасову теку і класифікувати вміст
     * overrides/ (та client-overrides/) для деплою.
     */
    public static ExtractionResult extract(Path mrpackFile, String packId) throws IOException {
        Path tmpDir = Files.createTempDirectory("pm-extract-" + safeName(packId) + "-");

        try (ZipInputStream zis = new ZipInputStream(Files.newInputStream(mrpackFile))) {
            ZipEntry entry;
            while ((entry = zis.getNextEntry()) != null) {
                Path target = tmpDir.resolve(entry.getName()).normalize();
                if (!target.startsWith(tmpDir)) {
                    zis.closeEntry();
                    continue; // ZIP path traversal guard
                }
                if (entry.isDirectory()) {
                    Files.createDirectories(target);
                } else {
                    if (target.getParent() != null) Files.createDirectories(target.getParent());
                    Files.write(target, zis.readAllBytes());
                }
                zis.closeEntry();
            }
        }

        List<String> mrpackFilePaths = new ArrayList<>();
        Path indexPath = tmpDir.resolve("modrinth.index.json");
        if (Files.exists(indexPath)) {
            // Лише шляхи потрібні тут для фільтрації "що вже покрито files[]";
            // повний парсинг (хеші/downloads/env) робить ManifestGeneratorV2.
            mrpackFilePaths = extractIndexPaths(indexPath);
        }
        Set<String> indexPathSet = new HashSet<>(mrpackFilePaths);

        List<OverrideFile> overrideFiles = new ArrayList<>();
        List<OverrideFolder> overrideFolders = new ArrayList<>();
        List<BundledModFile> bundledMods = new ArrayList<>();

        for (String overridesDirName : new String[]{"overrides", "client-overrides", "client_overrides"}) {
            Path overridesDir = tmpDir.resolve(overridesDirName);
            if (!Files.isDirectory(overridesDir)) continue;

            // mods/ — окремі bundled-моди, кожен jar незалежний
            Path modsDir = overridesDir.resolve("mods");
            if (Files.isDirectory(modsDir)) {
                try (var stream = Files.list(modsDir)) {
                    for (Path modFile : (Iterable<Path>) stream::iterator) {
                        if (Files.isRegularFile(modFile) && modFile.toString().endsWith(".jar")) {
                            bundledMods.add(new BundledModFile(
                                modFile.getFileName().toString(), modFile));
                        }
                    }
                }
            }

            // Top-level всередині overrides/, крім mods/
            try (var stream = Files.list(overridesDir)) {
                for (Path child : (Iterable<Path>) stream::iterator) {
                    String name = child.getFileName().toString();
                    if (name.equals("mods")) continue;

                    if (Files.isDirectory(child)) {
                        overrideFolders.add(new OverrideFolder(name, child));
                    } else {
                        // Відносний шлях у "overrides/"-координатах (без префіксу
                        // самої теки overrides/client-overrides) — саме так це
                        // потрапляє в options.txt/servers.dat у корені збірки.
                        overrideFiles.add(new OverrideFile(name, child));
                    }
                }
            }
        }

        log.info("[{}] Розпаковано .mrpack: {} files[], {} override-файлів, " +
                "{} override-тек, {} bundled-модів",
            packId, mrpackFilePaths.size(), overrideFiles.size(),
            overrideFolders.size(), bundledMods.size());

        return new ExtractionResult(tmpDir, mrpackFilePaths, overrideFiles, overrideFolders, bundledMods);
    }

    /** Видалити тимчасову розпаковану теку (викликати після завершення деплою збірки). */
    public static void cleanup(Path extractedRoot) {
        if (extractedRoot == null) return;
        try {
            if (!Files.exists(extractedRoot)) return;
            try (var walk = Files.walk(extractedRoot)) {
                walk.sorted(Comparator.reverseOrder())
                    .forEach(p -> { try { Files.delete(p); } catch (IOException ignored) {} });
            }
        } catch (IOException ignored) {}
    }

    private static List<String> extractIndexPaths(Path indexPath) {
        List<String> paths = new ArrayList<>();
        try {
            String json = Files.readString(indexPath, StandardCharsets.UTF_8);
            com.google.gson.JsonObject root =
                com.google.gson.JsonParser.parseString(json).getAsJsonObject();
            if (root.has("files") && root.get("files").isJsonArray()) {
                for (var el : root.getAsJsonArray("files")) {
                    var obj = el.getAsJsonObject();
                    if (obj.has("path") && !obj.get("path").isJsonNull()) {
                        paths.add(obj.get("path").getAsString());
                    }
                }
            }
        } catch (IOException e) {
            log.warn("Не вдалось прочитати modrinth.index.json: {}", e.getMessage());
        }
        return paths;
    }

    private static String safeName(String s) {
        return s == null ? "pack" : s.replaceAll("[^a-zA-Z0-9._-]", "_");
    }
}
