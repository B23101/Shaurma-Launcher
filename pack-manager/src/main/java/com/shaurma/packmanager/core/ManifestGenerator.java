package com.shaurma.packmanager.core;

import com.google.gson.*;
import com.google.gson.annotations.SerializedName;
import com.shaurma.packmanager.config.PackManagerConfig;
import com.shaurma.packmanager.core.R2Client;
import com.shaurma.packmanager.model.PackProject;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.security.MessageDigest;
import java.time.Instant;
import java.util.*;
import java.util.zip.ZipInputStream;

/**
 * Генератор server-manifest.json
 *
 * ВАЖЛИВО: всі URL-поля містять ВІДНОСНІ шляхи (R2 ключі), без домену.
 * Лаунчер сам додає свій SERVER_BASE_URL при читанні маніфесту.
 * Це дозволяє змінювати домен тільки в лаунчері, не перегенеруючи маніфест.
 *
 * Алгоритм:
 *  1. Для кожної збірки — завантажити .mrpack, розпакувати modrinth.index.json
 *  2. Розділити файли на hashMods (є downloads[]) і bundledMods (немає downloads[])
 *  3. Для bundledMods — перевірити наявність у mod-storage/, отримати fileSize
 *  4. Для force_archive папок — перевірити архіви на R2, взяти archiveSize
 *  5. Зібрати onMissingFiles зі списку on_missing правил
 *  6. Серіалізувати і зберегти
 */
public class ManifestGenerator {

    private static final Logger log = LoggerFactory.getLogger(ManifestGenerator.class);
    private static final Gson   GSON = new GsonBuilder().setPrettyPrinting().create();

    private final R2Client r2;

    public ManifestGenerator(R2Client r2) {
        this.r2 = r2;
    }

    /** Результат генерації */
    public record GenerationResult(
        String json,
        String outputPath,
        List<String> warnings,
        boolean success
    ) {
        public static GenerationResult error(String msg) {
            return new GenerationResult(null, null, List.of(msg), false);
        }
    }

    /**
     * Генерує і завантажує на R2:
     *   server-manifest.json          ← індекс збірок (лаунчер читає першим)
     *   {packId}/pack-manifest.json   ← деталі, хеші, правила
     *   {packId}/icon.png             ← якщо вказано iconPath
     *   {packId}/background.jpg       ← якщо вказано backgroundPath
     *
     * Всі URL-поля — відносні шляхи (ключі R2).
     * Лаунчер додає SERVER_BASE_URL самостійно.
     */
    public GenerationResult generate(List<PackProject> packs) {
        List<String> warnings = new ArrayList<>();

        try {
            // ── Завантажуємо існуючий server-manifest.json, щоб не втратити
            // записи інших збірок, які не входять у поточний deploy ─────────
            Map<String, JsonObject> existingById = new LinkedHashMap<>();
            List<String> existingOrder = new ArrayList<>();
            try {
                String existingJson = r2.getString("server-manifest.json");
                if (existingJson != null && !existingJson.isBlank()) {
                    JsonObject existingRoot = JsonParser.parseString(existingJson).getAsJsonObject();
                    if (existingRoot.has("packs") && existingRoot.get("packs").isJsonArray()) {
                        for (JsonElement el : existingRoot.getAsJsonArray("packs")) {
                            JsonObject obj = el.getAsJsonObject();
                            String id = obj.has("id") ? obj.get("id").getAsString() : null;
                            if (id != null) {
                                existingById.put(id, obj);
                                existingOrder.add(id);
                            }
                        }
                    }
                }
            } catch (Exception e) {
                warnings.add("Не вдалось завантажити існуючий server-manifest.json " +
                    "(можливо, перший deploy): " + e.getMessage());
            }

            for (PackProject pack : packs) {
                log.info("Генерація маніфесту для: {}", pack.getId());

                PackManifestJson packJson = generateForPack(pack, warnings);
                if (packJson == null) continue;

                // Upload {packId}/pack-manifest.json
                String packManifestKey  = "packs/" + pack.getId() + "/pack-manifest.json";
                String packManifestJson = GSON.toJson(packJson);
                R2Client.UploadResult pmResult = r2.putString(packManifestKey,
                    packManifestJson, "application/json");
                if (!pmResult.success()) {
                    warnings.add("[" + pack.getId() + "] pack-manifest.json upload failed: "
                        + pmResult.error());
                } else {
                    log.info("✓ {}", packManifestKey);
                }

                // Upload icon.png — зберігаємо ВІДНОСНИЙ шлях
                String iconPath = null;
                if (pack.getIconPath() != null && !pack.getIconPath().isBlank()) {
                    Path iconFilePath = Path.of(pack.getIconPath());
                    if (Files.exists(iconFilePath)) {
                        String iconKey = "packs/" + pack.getId() + "/icon.png";
                        R2Client.UploadResult ir = r2.upload(iconFilePath, iconKey, null);
                        if (ir.success()) {
                            iconPath = iconKey; // відносний шлях
                            log.info("✓ {}", iconKey);
                        } else {
                            warnings.add("[" + pack.getId() + "] icon.png upload failed: "
                                + ir.error());
                        }
                    }
                }

                // Upload background — зберігаємо ВІДНОСНИЙ шлях
                String backgroundPath = null;
                if (pack.getBackgroundPath() != null && !pack.getBackgroundPath().isBlank()) {
                    Path bgFilePath = Path.of(pack.getBackgroundPath());
                    if (Files.exists(bgFilePath)) {
                        String fileName = bgFilePath.getFileName().toString();
                        String ext = fileName.contains(".")
                            ? fileName.substring(fileName.lastIndexOf('.') + 1) : "jpg";
                        String bgKey = "packs/" + pack.getId() + "/background." + ext;
                        R2Client.UploadResult br = r2.upload(bgFilePath, bgKey, null);
                        if (br.success()) {
                            backgroundPath = bgKey; // відносний шлях
                            log.info("✓ {}", bgKey);
                        } else {
                            warnings.add("[" + pack.getId() + "] background upload failed: "
                                + br.error());
                        }
                    }
                }

                // Якщо файл не завантажувався в цьому deploy (не вказано локально),
                // зберігаємо попереднє значення з існуючого маніфесту, щоб не
                // загубити icon/background при деплої без них.
                JsonObject prevEntry = existingById.get(pack.getId());
                String prevIconPath = prevEntry != null && prevEntry.has("iconPath")
                    && !prevEntry.get("iconPath").isJsonNull()
                    ? prevEntry.get("iconPath").getAsString() : null;
                String prevBackgroundPath = prevEntry != null && prevEntry.has("backgroundPath")
                    && !prevEntry.get("backgroundPath").isJsonNull()
                    ? prevEntry.get("backgroundPath").getAsString() : null;

                if (iconPath == null && prevIconPath != null) {
                    iconPath = prevIconPath;
                }
                if (backgroundPath == null && prevBackgroundPath != null) {
                    backgroundPath = prevBackgroundPath;
                }

                // Якщо новий фон завантажено з іншим розширенням (наприклад
                // background.jpg → background.png), старий файл лишається
                // "сирітським" на R2 — видаляємо його.
                if (backgroundPath != null && prevBackgroundPath != null
                        && !backgroundPath.equals(prevBackgroundPath)) {
                    try {
                        if (r2.delete(prevBackgroundPath)) {
                            log.info("✓ Видалено застарілий фон: {}", prevBackgroundPath);
                        }
                    } catch (Exception e) {
                        warnings.add("[" + pack.getId() + "] Не вдалось видалити старий фон "
                            + prevBackgroundPath + ": " + e.getMessage());
                    }
                }
                if (iconPath != null && prevIconPath != null
                        && !iconPath.equals(prevIconPath)) {
                    try {
                        if (r2.delete(prevIconPath)) {
                            log.info("✓ Видалено застарілу іконку: {}", prevIconPath);
                        }
                    } catch (Exception e) {
                        warnings.add("[" + pack.getId() + "] Не вдалось видалити стару іконку "
                            + prevIconPath + ": " + e.getMessage());
                    }
                }

                // Додаємо до індексу — всі URL-поля як відносні шляхи
                PackIndexEntry entry = new PackIndexEntry();
                entry.id              = pack.getId();
                entry.name            = pack.getName();
                entry.mcVersion       = pack.getMcVersion();
                entry.loader          = pack.getLoader();
                entry.loaderVersion   = pack.getLoaderVersion();
                entry.description     = pack.getDescription();
                entry.tags            = pack.getTags();
                entry.iconPath        = iconPath;
                entry.backgroundPath  = backgroundPath;
                // Отримуємо ETag для іконки та фону — щоб лаунчер міг інвалідувати кеш при змінах
                if (iconPath != null) {
                    try { entry.iconEtag = r2.head(iconPath).etag(); }
                    catch (Exception e) { warnings.add("[" + pack.getId() + "] Не вдалось отримати ETag icon: " + e.getMessage()); }
                }
                if (backgroundPath != null) {
                    try { entry.backgroundEtag = r2.head(backgroundPath).etag(); }
                    catch (Exception e) { warnings.add("[" + pack.getId() + "] Не вдалось отримати ETag background: " + e.getMessage()); }
                }
                entry.packManifestPath = packManifestKey;
                entry.mrpackPath      = packJson.mrpackPath;
                entry.mrpackEtag      = packJson.mrpackEtag;
                entry.mrpackSize      = packJson.mrpackSize;
                entry.syncRules       = packJson.syncRules;
                entry.hashMods        = packJson.hashMods;
                entry.bundledMods     = packJson.bundledMods;
                entry.archives        = packJson.archives;
                entry.onMissingFiles  = packJson.onMissingFiles;
                entry.serverIp        = pack.getServerIp();
                entry.accentColor     = pack.getAccentColor();

                // ── updatedAt: змінюється лише якщо змінився контент самого
                // модпаку (mrpack/мод-листи/архіви/sync-правила/версії), а не
                // через зміну іконки/фону/опису/тегів ───────────────────────
                String newContentHash = contentHash(entry);
                String prevContentHash = prevEntry != null && prevEntry.has("contentHash")
                    && !prevEntry.get("contentHash").isJsonNull()
                    ? prevEntry.get("contentHash").getAsString() : null;
                String prevUpdatedAt = prevEntry != null && prevEntry.has("updatedAt")
                    && !prevEntry.get("updatedAt").isJsonNull()
                    ? prevEntry.get("updatedAt").getAsString() : null;

                if (prevUpdatedAt != null && newContentHash.equals(prevContentHash)) {
                    entry.updatedAt = prevUpdatedAt;
                } else {
                    entry.updatedAt = Instant.now().toString();
                }
                entry.contentHash = newContentHash;

                JsonObject newEntryJson = GSON.toJsonTree(entry).getAsJsonObject();
                existingById.put(pack.getId(), newEntryJson);
                if (!existingOrder.contains(pack.getId())) {
                    existingOrder.add(pack.getId());
                }
            }

            // ── Зібрати фінальний список: усі існуючі збірки (оновлені або
            // незмінні) у попередньому порядку ────────────────────────────
            JsonObject serverManifestRoot = new JsonObject();
            serverManifestRoot.addProperty("version", 2);
            serverManifestRoot.addProperty("generatedAt", Instant.now().toString());
            JsonArray packsArray = new JsonArray();
            for (String id : existingOrder) {
                JsonObject obj = existingById.get(id);
                if (obj != null) packsArray.add(obj);
            }
            serverManifestRoot.add("packs", packsArray);

            // Upload server-manifest.json
            String serverJson  = GSON.toJson(serverManifestRoot);
            String localOutput = resolveOutputPath();
            Files.writeString(Path.of(localOutput), serverJson);

            R2Client.UploadResult smResult = r2.putString("server-manifest.json",
                serverJson, "application/json");
            if (!smResult.success()) {
                warnings.add("server-manifest.json upload failed: " + smResult.error());
            } else {
                log.info("✓ server-manifest.json");
            }

            return new GenerationResult(serverJson, localOutput, warnings, true);

        } catch (Exception e) {
            log.error("Помилка генерації маніфесту", e);
            return GenerationResult.error("Помилка: " + e.getMessage());
        }
    }

    /**
     * Хеш контенту збірки (без icon/background/description/tags), який
     * визначає, чи варто оновлювати "дату оновлення" модпаку.
     * Зміна лише картинки/опису НЕ повинна змінювати updatedAt.
     */
    private String contentHash(PackIndexEntry entry) {
        StringBuilder sb = new StringBuilder();
        sb.append(entry.mcVersion).append('|')
          .append(entry.loader).append('|')
          .append(entry.loaderVersion).append('|')
          .append(entry.mrpackEtag).append('|')
          .append(entry.mrpackSize).append('|')
          .append(GSON.toJson(entry.syncRules)).append('|')
          .append(GSON.toJson(entry.hashMods)).append('|')
          .append(GSON.toJson(entry.bundledMods)).append('|')
          .append(GSON.toJson(entry.archives)).append('|')
          .append(GSON.toJson(entry.onMissingFiles));
        return Integer.toHexString(sb.toString().hashCode());
    }

    private PackManifestJson generateForPack(PackProject pack, List<String> warnings) throws IOException {
        PackManifestJson pm = new PackManifestJson();
        pm.id            = pack.getId();
        pm.name          = pack.getName();
        pm.mcVersion     = pack.getMcVersion();
        pm.loader        = pack.getLoader();
        pm.loaderVersion = pack.getLoaderVersion();
        pm.syncRules     = pack.getSyncRules();
        pm.serverIp      = pack.getServerIp();
        pm.accentColor   = pack.getAccentColor();

        // ── Авто-визначення mcVersion/loader/loaderVersion з .mrpack ──────
        // modrinth.index.json -> dependencies є джерелом правди; ручний вибір
        // в UI не потрібен — аналізуємо модпак напряму.
        Path mrpackForAnalysis = resolveMrpackPath(pack);
        if (mrpackForAnalysis != null && Files.exists(mrpackForAnalysis)) {
            MrpackAnalyzer.AnalysisResult analysis = MrpackAnalyzer.analyze(mrpackForAnalysis);
            if (analysis.success()) {
                pm.mcVersion = analysis.mcVersion();
                if (analysis.loader() != null) pm.loader = analysis.loader();
                if (analysis.loaderVersion() != null) pm.loaderVersion = analysis.loaderVersion();
            } else {
                warnings.add("[" + pack.getId() + "] Автоаналіз loader/версії не вдався: "
                    + analysis.error() + " — використано збережені значення");
            }
        }

        // mrpackPath — відносний ключ R2 (лаунчер додасть базовий URL)
        String storedMrpackPath = pack.getMrpackUrl();
        if (storedMrpackPath == null || storedMrpackPath.isBlank() || storedMrpackPath.startsWith("http")) {
            // Якщо збережено повний URL (старий формат) або порожньо — генеруємо відносний ключ
            pm.mrpackPath = "packs/" + pack.getId() + "/" + pack.getId() + ".mrpack";
            if (storedMrpackPath == null || storedMrpackPath.isBlank()) {
                warnings.add("[" + pack.getId() + "] mrpackUrl не вказано — " +
                    "підставлено очікуваний R2 шлях: " + pm.mrpackPath +
                    ". Виконайте Deploy щоб завантажити .mrpack на R2.");
            }
        } else {
            // Вже відносний шлях
            pm.mrpackPath = storedMrpackPath;
        }

        // ── Читаємо modrinth.index.json з mrpack ──────────────────────────
        Path mrpackFilePath = resolveMrpackPath(pack);
        if (mrpackFilePath == null || !Files.exists(mrpackFilePath)) {
            warnings.add("[" + pack.getId() + "] mrpack не знайдено: " + pack.getMrpackPath());
            pm.hashMods      = List.of();
            pm.bundledMods   = List.of();
            pm.archives      = List.of();
            pm.onMissingFiles = List.of();
            return pm;
        }

        // ETag і розмір mrpack
        try {
            R2Client.HeadResult mrpackHead = r2.head(pm.mrpackPath);
            pm.mrpackEtag = mrpackHead.etag();
            pm.mrpackSize = mrpackHead.size();
        } catch (Exception e) {
            warnings.add("[" + pack.getId() + "] Не вдалось отримати ETag mrpack: " + e.getMessage());
        }

        MrpackIndex index = parseMrpackIndex(mrpackFilePath);
        if (index == null) {
            warnings.add("[" + pack.getId() + "] Не вдалось розпакувати modrinth.index.json");
            pm.hashMods      = List.of();
            pm.bundledMods   = List.of();
            pm.archives      = List.of();
            pm.onMissingFiles = List.of();
            return pm;
        }

        // ── Розділити на hashMods і bundledMods ───────────────────────────
        pm.hashMods    = new ArrayList<>();
        pm.bundledMods = new ArrayList<>();

        if (index.files != null) {
            for (MrpackFile file : index.files) {
                if (!file.isClientSide()) continue;

                boolean hasUrl = file.downloads != null && !file.downloads.isEmpty();

                if (hasUrl) {
                    HashModJson hm = new HashModJson();
                    hm.fileName    = fileName(file.path);
                    hm.path        = file.path;
                    hm.sha1        = file.hashes != null ? file.hashes.sha1 : null;
                    hm.sha512      = file.hashes != null ? file.hashes.sha512 : null;
                    hm.fileSize    = file.fileSize;
                    hm.downloadUrl = file.downloads.get(0); // зовнішній URL (modrinth/curseforge)
                    hm.env         = file.env;
                    pm.hashMods.add(hm);
                } else {
                    String modName = fileName(file.path);
                    BundledModJson bm = new BundledModJson();
                    bm.fileName        = modName;
                    bm.modStoragePath  = buildModStoragePath(pack, modName); // відносний шлях
                    BundledModMeta meta = resolveBundledModMeta(pack, modName, mrpackFilePath, warnings);
                    bm.fileSize        = meta.size;
                    bm.sha256          = meta.sha256;
                    pm.bundledMods.add(bm);
                }
            }
        }

        // ── Bundled-моди з overrides/mods/ ────────────────────────────────
        Set<String> knownBundled = new HashSet<>();
        pm.bundledMods.forEach(b -> knownBundled.add(b.fileName));

        for (String modName : listOverridesMods(mrpackFilePath)) {
            boolean isHash = pm.hashMods.stream().anyMatch(h -> h.fileName.equals(modName));
            if (!isHash && !knownBundled.contains(modName)) {
                BundledModJson bm = new BundledModJson();
                bm.fileName       = modName;
                bm.modStoragePath = buildModStoragePath(pack, modName);
                BundledModMeta meta = resolveBundledModMeta(pack, modName, mrpackFilePath, warnings);
                bm.fileSize       = meta.size;
                bm.sha256         = meta.sha256;
                pm.bundledMods.add(bm);
            }
        }

        // ── Архіви (force_archive папки) ──────────────────────────────────
        pm.archives = buildArchiveList(pack, warnings);

        // ── onMissingFiles (on_missing правила) ───────────────────────────
        pm.onMissingFiles = buildOnMissingFiles(pack);

        log.info("[{}] hashMods={}, bundledMods={}, archives={}, onMissing={}",
            pack.getId(), pm.hashMods.size(), pm.bundledMods.size(),
            pm.archives.size(), pm.onMissingFiles.size());

        return pm;
    }

    // ── Допоміжні методи ─────────────────────────────────────────────────

    private List<ArchiveJson> buildArchiveList(PackProject pack, List<String> warnings) {
        List<ArchiveJson> result = new ArrayList<>();
        for (Map.Entry<String, String> entry : pack.getSyncRules().entrySet()) {
            if (!PackProject.RULE_FORCE_ARCHIVE.equals(entry.getValue())) continue;

            String localPath = entry.getKey();
            // Відносний ключ R2
            String archivePath = "packs/archives/" + pack.getId() + "/"
                + localPath.replace("/", "") + ".zip";

            ArchiveJson aj = new ArchiveJson();
            aj.localPath   = localPath;
            aj.archivePath = archivePath; // відносний шлях

            try {
                R2Client.HeadResult head = r2.head(archivePath);
                aj.archiveSize    = head.size();
                aj.compressedSize = head.size();
            } catch (Exception e) {
                warnings.add("[" + pack.getId() + "] Архів не знайдено на R2: " + archivePath
                    + " — спочатку виконайте Deploy");
                aj.archiveSize    = 0;
                aj.compressedSize = 0;
            }

            result.add(aj);
        }
        return result;
    }

    private List<OnMissingFileJson> buildOnMissingFiles(PackProject pack) {
        List<OnMissingFileJson> result = new ArrayList<>();
        for (Map.Entry<String, String> entry : pack.getSyncRules().entrySet()) {
            if (!PackProject.RULE_ON_MISSING.equals(entry.getValue())) continue;
            String path = entry.getKey();
            if (path.endsWith("/")) continue; // папки — файли додаються окремо

            OnMissingFileJson omf = new OnMissingFileJson();
            omf.localPath  = path;
            omf.sourcePath = "defaults/" + pack.getId() + "/" + path; // відносний шлях
            result.add(omf);
        }
        return result;
    }

    /** Відносний R2 ключ для bundled-моду */
    private String buildModStoragePath(PackProject pack, String modName) {
        return "packs/" + pack.getId() + "/mod-storage/" + modName;
    }

    private record BundledModMeta(long size, String sha256) {}

    private BundledModMeta resolveBundledModMeta(PackProject pack, String modName,
                                                  Path mrpackPath, List<String> warnings) {
        try (ZipInputStream zis = new ZipInputStream(Files.newInputStream(mrpackPath))) {
            java.util.zip.ZipEntry entry;
            while ((entry = zis.getNextEntry()) != null) {
                String name = entry.getName();
                if ((name.startsWith("overrides/mods/") || name.startsWith("client-overrides/mods/"))
                    && name.endsWith(modName)) {
                    byte[] bytes = zis.readAllBytes();
                    String sha256 = sha256Hex(bytes);
                    return new BundledModMeta(bytes.length, sha256);
                }
                zis.closeEntry();
            }
        } catch (IOException e) {
            warnings.add("Не вдалось прочитати bundled-мод: " + modName);
        }
        // Fallback: HEAD до R2 (тільки розмір, без SHA256)
        try {
            R2Client.HeadResult head = r2.head("packs/" + pack.getId() + "/mod-storage/" + modName);
            if (head.size() > 0) return new BundledModMeta(head.size(), null);
        } catch (Exception ignored) {}
        return new BundledModMeta(0, null);
    }

    private static String sha256Hex(byte[] data) {
        try {
            MessageDigest md = MessageDigest.getInstance("SHA-256");
            byte[] digest = md.digest(data);
            StringBuilder sb = new StringBuilder();
            for (byte b : digest) sb.append(String.format("%02x", b));
            return sb.toString();
        } catch (Exception e) {
            return null;
        }
    }

    private MrpackIndex parseMrpackIndex(Path mrpackPath) {
        try (ZipInputStream zis = new ZipInputStream(Files.newInputStream(mrpackPath))) {
            java.util.zip.ZipEntry entry;
            while ((entry = zis.getNextEntry()) != null) {
                if ("modrinth.index.json".equals(entry.getName())) {
                    String json = new String(zis.readAllBytes(), StandardCharsets.UTF_8);
                    return GSON.fromJson(json, MrpackIndex.class);
                }
                zis.closeEntry();
            }
        } catch (IOException e) {
            log.error("Помилка читання mrpack: {}", e.getMessage());
        }
        return null;
    }

    private Set<String> listOverridesMods(Path mrpackPath) {
        Set<String> names = new LinkedHashSet<>();
        try (ZipInputStream zis = new ZipInputStream(Files.newInputStream(mrpackPath))) {
            java.util.zip.ZipEntry entry;
            while ((entry = zis.getNextEntry()) != null) {
                String name = entry.getName();
                if (!entry.isDirectory()
                    && (name.startsWith("overrides/mods/") || name.startsWith("client-overrides/mods/"))
                    && name.endsWith(".jar")) {
                    names.add(name.substring(name.lastIndexOf('/') + 1));
                }
                zis.closeEntry();
            }
        } catch (IOException ignored) {}
        return names;
    }

    private Path resolveMrpackPath(PackProject pack) {
        if (pack.getMrpackPath() != null && !pack.getMrpackPath().isBlank()) {
            return Path.of(pack.getMrpackPath());
        }
        String dataDir = PackManagerConfig.settings().dataDirectory();
        if (dataDir != null && !dataDir.isBlank()) {
            return Path.of(dataDir, pack.getId() + ".mrpack");
        }
        return null;
    }

    private String resolveOutputPath() {
        String dataDir = PackManagerConfig.settings().dataDirectory();
        if (dataDir != null && !dataDir.isBlank()) {
            return dataDir + "/server-manifest.json";
        }
        return "server-manifest.json";
    }

    private static String fileName(String path) {
        int slash = path.lastIndexOf('/');
        return slash >= 0 ? path.substring(slash + 1) : path;
    }

    // ── JSON-структури для серіалізації ───────────────────────────────────

    /** server-manifest.json — індекс всіх збірок, читає лаунчер першим */
    private static class ServerManifestJson {
        int version;
        String generatedAt;
        List<PackIndexEntry> packs;
    }

    /**
     * Запис збірки в server-manifest.json.
     * Всі *Path поля — відносні R2 ключі. Лаунчер додає SERVER_BASE_URL.
     * Виняток: hashMods[].downloadUrl — зовнішні URL (modrinth/curseforge), не чіпати.
     */
    private static class PackIndexEntry {
        String id, name, mcVersion, loader, loaderVersion;
        String description;
        List<String> tags;
        String iconPath;
        String backgroundPath;
        String iconEtag;
        String backgroundEtag;
        String packManifestPath;
        String mrpackPath;
        String mrpackEtag;
        long   mrpackSize;
        String updatedAt;
        String contentHash;
        String serverIp;
        String accentColor;
        Map<String, String>       syncRules;
        List<HashModJson>         hashMods;
        List<BundledModJson>      bundledMods;
        List<ArchiveJson>         archives;
        List<OnMissingFileJson>   onMissingFiles;
    }

    /** {packId}/pack-manifest.json */
    private static class PackManifestJson {
        String id, name, mcVersion, loader, loaderVersion;
        String mrpackPath, mrpackEtag;
        long mrpackSize;
        Map<String, String> syncRules;
        List<HashModJson>       hashMods;
        List<BundledModJson>    bundledMods;
        List<ArchiveJson>       archives;
        List<OnMissingFileJson> onMissingFiles;
        String serverIp;
        String accentColor;
    }

    private static class HashModJson {
        String fileName, path, sha1, sha512;
        String downloadUrl; // зовнішній URL — залишається як є
        long fileSize;
        Map<String, String> env;
    }

    private static class BundledModJson {
        String fileName, modStoragePath; // відносний шлях
        long fileSize;
        @SerializedName("sha256") String sha256;
    }

    private static class ArchiveJson {
        String localPath, archivePath; // archivePath — відносний шлях
        long archiveSize, compressedSize;
    }

    private static class OnMissingFileJson {
        String localPath, sourcePath; // sourcePath — відносний шлях
    }

    // ── modrinth.index.json структури ─────────────────────────────────────

    private static class MrpackIndex {
        List<MrpackFile> files;
    }

    private static class MrpackFile {
        String path;
        Hashes hashes;
        Map<String, String> env;
        List<String> downloads;
        long fileSize;

        boolean isClientSide() {
            if (env == null) return true;
            String side = env.get("client");
            return side == null || !side.equals("unsupported");
        }
    }

    private static class Hashes {
        String sha1, sha512;
    }
}
