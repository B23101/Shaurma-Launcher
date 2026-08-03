package com.shaurma.packmanager.core;

import com.google.gson.*;
import com.shaurma.packmanager.model.PackProject;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.time.Instant;
import java.util.*;
import java.util.zip.ZipInputStream;

/**
 * Генератор маніфестів V2 (DOWNLOAD_SYNC_DESIGN_V2.md, розділ 2 і 3).
 *
 * На відміну від старого ManifestGenerator (1 великий server-manifest.json),
 * тут:
 *   - packs/<id>/manifest.json  — ПОВНИЙ маніфест ОДНІЄЇ збірки
 *   - packs-index.json          — легкий корінний індекс з посиланнями на
 *                                  manifest.json КОЖНОЇ збірки + їхніми ETag
 *
 * Кожен елемент hashMods[]/workerFiles[] несе SHA-хеш і updatedAt.
 * updatedAt елемента оновлюється ТІЛЬКИ якщо хеш реально змінився відносно
 * попереднього manifest.json цієї збірки на R2 (розділ 3.2 V2-документа) —
 * це і є захист від "фантомного оновлення".
 */
public class ManifestGeneratorV2 {

    private static final Logger log = LoggerFactory.getLogger(ManifestGeneratorV2.class);
    private static final Gson GSON = new GsonBuilder().setPrettyPrinting().create();

    public static final int SCHEMA_VERSION = 3;

    private final R2Client r2;

    public ManifestGeneratorV2(R2Client r2) {
        this.r2 = r2;
    }

    public record GenerationResult(boolean success, List<String> warnings, PackManifestJson manifest) {
        public static GenerationResult error(String msg) {
            return new GenerationResult(false, List.of(msg), null);
        }
    }

    /**
     * Деплой ОДНІЄЇ збірки за новою схемою. Викликається з DeployServiceV2
     * після локальної розпаковки .mrpack (MrpackExtractor.extract).
     *
     * @param onFileEvent    колбек для кожного залитого/пропущеного файлу
     *                       (opis, uploaded?, bytes)
     */
    public GenerationResult deployPack(
            PackProject pack,
            MrpackExtractor.ExtractionResult extraction,
            java.util.function.Consumer<String> onLog) {

        List<String> warnings = new ArrayList<>();
        try {
            // ── Прочитати попередній manifest.json цієї збірки (якщо є) ────
            JsonObject prevManifest = null;
            try {
                String prevJson = r2.getString("packs/" + pack.getId() + "/manifest.json");
                if (prevJson != null && !prevJson.isBlank()) {
                    prevManifest = JsonParser.parseString(prevJson).getAsJsonObject();
                }
            } catch (Exception e) {
                warnings.add("Не вдалось прочитати попередній manifest.json (можливо, перший деплой): "
                    + e.getMessage());
            }
            Map<String, JsonObject> prevWorkerFilesByPath = indexByPath(prevManifest, "workerFiles");

            // ── mcVersion/loader/loaderVersion — з modrinth.index.json ─────
            Path mrpackPath = resolveMrpackPath(pack);
            MrpackAnalyzer.AnalysisResult analysis = mrpackPath != null
                ? MrpackAnalyzer.analyze(mrpackPath) : MrpackAnalyzer.AnalysisResult.error("no mrpack");
            String mcVersion = analysis.success() ? analysis.mcVersion() : pack.getMcVersion();
            String loader = analysis.success() && analysis.loader() != null ? analysis.loader() : pack.getLoader();
            String loaderVersion = analysis.success() && analysis.loaderVersion() != null
                ? analysis.loaderVersion() : pack.getLoaderVersion();

            // ── hashMods з modrinth.index.json (files[] з downloads[]) ─────
            MrpackIndexFull index = parseFullIndex(mrpackPath);
            List<HashModJson> hashMods = new ArrayList<>();
            Set<String> hashModNames = new HashSet<>();
            if (index != null && index.files != null) {
                for (MrpackFileFull f : index.files) {
                    if (!f.isClientSide()) continue;
                    boolean hasUrl = f.downloads != null && !f.downloads.isEmpty();
                    if (!hasUrl) continue;
                    HashModJson hm = new HashModJson();
                    hm.fileName = fileName(f.path);
                    hm.sha1 = f.hashes != null ? f.hashes.sha1 : null;
                    hm.sha512 = f.hashes != null ? f.hashes.sha512 : null;
                    hm.fileSize = f.fileSize;
                    hm.downloadUrl = f.downloads.get(0);
                    // hashMods йдуть з зовнішнього CDN (Modrinth/CurseForge) — їхній
                    // updatedAt контролює не Pack Manager; беремо "зараз" лише коли
                    // елемент новий, інакше зберігаємо попередній.
                    hm.updatedAt = resolvePrevUpdatedAt(prevManifest, "hashMods", hm.fileName);
                    if (hm.updatedAt == null) hm.updatedAt = Instant.now().toString();
                    hashMods.add(hm);
                    hashModNames.add(hm.fileName);
                }
            }

            // ── workerFiles: bundled-моди + override-файли + override-теки ─
            List<WorkerFileJson> workerFiles = new ArrayList<>();
            int uploaded = 0, skipped = 0, errored = 0;

            // bundled-моди (кожен jar незалежний, крім тих що вже в hashMods)
            for (MrpackExtractor.BundledModFile bm : extraction.bundledMods()) {
                if (hashModNames.contains(bm.fileName())) continue; // вже покрито hashMods
                String r2Key = "packs/" + pack.getId() + "/mods/" + bm.fileName();
                WorkerFileJson wf = buildWorkerFile(
                    "bundled-mod", bm.fileName(), bm.absolutePath(), r2Key,
                    prevWorkerFilesByPath, onLog, warnings);
                workerFiles.add(wf);
                if ("uploaded".equals(wf.transient_status)) uploaded++;
                else if ("skipped".equals(wf.transient_status)) skipped++;
                else errored++;
            }

            // override-файли (top-level, не архівуються)
            for (MrpackExtractor.OverrideFile of : extraction.overrideFiles()) {
                String r2Key = "packs/" + pack.getId() + "/overrides/" + of.localPath();
                WorkerFileJson wf = buildWorkerFile(
                    "override-file", of.localPath(), of.absolutePath(), r2Key,
                    prevWorkerFilesByPath, onLog, warnings);
                workerFiles.add(wf);
                if ("uploaded".equals(wf.transient_status)) uploaded++;
                else if ("skipped".equals(wf.transient_status)) skipped++;
                else errored++;
            }

            // override-теки (архівуються FolderArchiver'ом як одне ціле — "bubble mod")
            for (MrpackExtractor.OverrideFolder folder : extraction.overrideFolders()) {
                Path tmpZip = FolderArchiver.tempArchivePath(pack.getId(), folder.localName());
                FolderArchiver.ArchiveResult ar = FolderArchiver.archive(folder.absolutePath(), tmpZip, null);
                if (!ar.success()) {
                    warnings.add("Архівація override-теки '" + folder.localName() + "' не вдалась: " + ar.error());
                    errored++;
                    continue;
                }
                String r2Key = "packs/" + pack.getId() + "/override-folders/" + folder.localName() + ".zip";
                WorkerFileJson wf = buildWorkerFile(
                    "override-folder", folder.localName(), tmpZip, r2Key,
                    prevWorkerFilesByPath, onLog, warnings);
                wf.fileCount = ar.fileCount();
                wf.archiveSize = wf.fileSize;
                workerFiles.add(wf);
                if ("uploaded".equals(wf.transient_status)) uploaded++;
                else if ("skipped".equals(wf.transient_status)) skipped++;
                else errored++;
                try { Files.deleteIfExists(tmpZip); } catch (IOException ignored) {}
            }

            // ── onMissingFiles: беремо з правил on_missing (localPath -> R2-шлях у overrides/) ─
            List<OnMissingFileJson> onMissing = new ArrayList<>();
            for (Map.Entry<String, String> e : pack.getSyncRules().entrySet()) {
                if (!PackProject.RULE_ON_MISSING.equals(e.getValue())) continue;
                String localPath = e.getKey();
                if (localPath.endsWith("/")) continue;
                // Шукаємо серед вже класифікованих override-файлів той самий шлях
                boolean found = false;
                for (WorkerFileJson wf : workerFiles) {
                    if ("override-file".equals(wf.kind) && localPath.equals(wf.localPath)) {
                        OnMissingFileJson omf = new OnMissingFileJson();
                        omf.localPath = localPath;
                        omf.path = wf.path;
                        omf.sha256 = wf.sha256;
                        onMissing.add(omf);
                        found = true;
                        break;
                    }
                }
                if (!found) {
                    warnings.add("on_missing правило '" + localPath +
                        "' не знайдено серед override-файлів (перевірте, що файл top-level в overrides/)");
                }
            }

            // ── Зібрати PackManifestJson ────────────────────────────────────
            PackManifestJson pm = new PackManifestJson();
            pm.id = pack.getId();
            pm.schemaVersion = SCHEMA_VERSION;
            pm.mcVersion = mcVersion;
            pm.loader = loader;
            pm.loaderVersion = loaderVersion;
            pm.hashMods = hashMods;
            pm.workerFiles = workerFiles;
            pm.onMissingFiles = onMissing;
            pm.serverIp = pack.getServerIp();
            pm.accentColor = pack.getAccentColor();

            // contentHash + updatedAt на рівні всієї збірки (тільки для метаданих
            // packs-index.json — індивідуальні updatedAt файлів уже коректні вище)
            String newContentHash = computeContentHash(pm);
            String prevContentHash = prevManifest != null && prevManifest.has("contentHash")
                && !prevManifest.get("contentHash").isJsonNull()
                ? prevManifest.get("contentHash").getAsString() : null;
            String prevUpdatedAt = prevManifest != null && prevManifest.has("updatedAt")
                && !prevManifest.get("updatedAt").isJsonNull()
                ? prevManifest.get("updatedAt").getAsString() : null;

            pm.contentHash = newContentHash;
            pm.updatedAt = (prevUpdatedAt != null && newContentHash.equals(prevContentHash))
                ? prevUpdatedAt : Instant.now().toString();

            // Прибрати службове поле перед серіалізацією
            String manifestJson = GSON.toJson(stripTransient(pm));
            String manifestKey = "packs/" + pack.getId() + "/manifest.json";
            R2Client.UploadResult ur = r2.putString(manifestKey, manifestJson, "application/json");
            if (!ur.success()) {
                warnings.add("Не вдалось залити manifest.json: " + ur.error());
                return new GenerationResult(false, warnings, pm);
            }

            log(onLog, String.format(
                "[%s] manifest.json: %d hashMods, %d workerFiles (uploaded=%d, skipped=%d, errored=%d), %d onMissing",
                pack.getId(), hashMods.size(), workerFiles.size(), uploaded, skipped, errored, onMissing.size()));

            return new GenerationResult(errored == 0, warnings, pm);

        } catch (Exception e) {
            log.error("Deploy manifest V2 failed for {}", pack.getId(), e);
            return GenerationResult.error("Помилка генерації manifest.json: " + e.getMessage());
        }
    }

    /**
     * Оновити (read-modify-write) запис однієї збірки в packs-index.json,
     * не чіпаючи записи інших збірок.
     */
    public List<String> updatePacksIndex(PackProject pack, PackManifestJson pm) {
        List<String> warnings = new ArrayList<>();
        try {
            JsonObject indexRoot;
            JsonArray packsArray;
            try {
                String existing = r2.getString("packs-index.json");
                if (existing != null && !existing.isBlank()) {
                    indexRoot = JsonParser.parseString(existing).getAsJsonObject();
                    packsArray = indexRoot.has("packs") && indexRoot.get("packs").isJsonArray()
                        ? indexRoot.getAsJsonArray("packs") : new JsonArray();
                } else {
                    indexRoot = new JsonObject();
                    packsArray = new JsonArray();
                }
            } catch (Exception e) {
                indexRoot = new JsonObject();
                packsArray = new JsonArray();
                warnings.add("packs-index.json не знайдено — створюємо новий (перший деплой?)");
            }

            String manifestKey = "packs/" + pack.getId() + "/manifest.json";
            String manifestEtag = null;
            try { manifestEtag = r2.head(manifestKey).etag(); }
            catch (Exception e) { warnings.add("Не вдалось отримати ETag manifest.json: " + e.getMessage()); }

            // icon/background — заливаються DeployServiceV2 окремо; читаємо ETag якщо є
            String iconPath = "packs/" + pack.getId() + "/icon.png";
            String iconEtag = r2.exists(iconPath) ? safeEtag(iconPath) : null;
            String bgPath = null;
            for (String ext : new String[]{"jpg", "jpeg", "png", "webp"}) {
                String candidate = "packs/" + pack.getId() + "/background." + ext;
                if (r2.exists(candidate)) { bgPath = candidate; break; }
            }
            String bgEtag = bgPath != null ? safeEtag(bgPath) : null;

            JsonObject entry = new JsonObject();
            entry.addProperty("id", pack.getId());
            entry.addProperty("name", pack.getName());
            entry.addProperty("description", pack.getDescription());
            entry.addProperty("mcVersion", pm.mcVersion);
            entry.addProperty("loader", pm.loader);
            entry.addProperty("loaderVersion", pm.loaderVersion);
            if (r2.exists(iconPath)) entry.addProperty("iconPath", iconPath); else entry.add("iconPath", JsonNull.INSTANCE);
            entry.add("iconEtag", iconEtag != null ? new JsonPrimitive(iconEtag) : JsonNull.INSTANCE);
            if (bgPath != null) entry.addProperty("backgroundPath", bgPath); else entry.add("backgroundPath", JsonNull.INSTANCE);
            entry.add("backgroundEtag", bgEtag != null ? new JsonPrimitive(bgEtag) : JsonNull.INSTANCE);
            JsonArray tags = new JsonArray();
            if (pack.getTags() != null) pack.getTags().forEach(tags::add);
            entry.add("tags", tags);
            entry.addProperty("accentColor", pack.getAccentColor());
            entry.addProperty("serverIp", pack.getServerIp());
            entry.addProperty("manifestPath", manifestKey);
            entry.add("manifestEtag", manifestEtag != null ? new JsonPrimitive(manifestEtag) : JsonNull.INSTANCE);
            entry.addProperty("updatedAt", pm.updatedAt);

            // Замінити існуючий запис або додати новий, зберігаючи порядок решти
            JsonArray newArray = new JsonArray();
            boolean replaced = false;
            for (JsonElement el : packsArray) {
                JsonObject obj = el.getAsJsonObject();
                if (obj.has("id") && pack.getId().equals(obj.get("id").getAsString())) {
                    newArray.add(entry);
                    replaced = true;
                } else {
                    newArray.add(obj);
                }
            }
            if (!replaced) newArray.add(entry);

            JsonObject newRoot = new JsonObject();
            newRoot.addProperty("version", SCHEMA_VERSION);
            newRoot.addProperty("generatedAt", Instant.now().toString());
            newRoot.add("packs", newArray);

            R2Client.UploadResult ur = r2.putString("packs-index.json",
                GSON.toJson(newRoot), "application/json");
            if (!ur.success()) {
                warnings.add("Не вдалось залити packs-index.json: " + ur.error());
            }
        } catch (Exception e) {
            warnings.add("updatePacksIndex помилка: " + e.getMessage());
        }
        return warnings;
    }

    // ── Побудова одного WorkerFile-запису з hash+date порівнянням ─────────

    private WorkerFileJson buildWorkerFile(
            String kind, String localOrFileName, Path localAbsPath, String r2Key,
            Map<String, JsonObject> prevByPath,
            java.util.function.Consumer<String> onLog, List<String> warnings) {

        WorkerFileJson wf = new WorkerFileJson();
        wf.kind = kind;
        if ("bundled-mod".equals(kind)) wf.fileName = localOrFileName;
        else wf.localPath = localOrFileName;
        wf.path = r2Key;

        String sha256;
        long size;
        try {
            byte[] bytes = Files.readAllBytes(localAbsPath);
            sha256 = sha256Hex(bytes);
            size = bytes.length;
        } catch (IOException e) {
            warnings.add("Не вдалось прочитати файл для хешування: " + localAbsPath + ": " + e.getMessage());
            wf.transient_status = "errored";
            return wf;
        }
        wf.sha256 = sha256;
        wf.fileSize = size;

        JsonObject prev = prevByPath.get(r2Key);
        String prevSha = prev != null && prev.has("sha256") && !prev.get("sha256").isJsonNull()
            ? prev.get("sha256").getAsString() : null;
        String prevUpdatedAt = prev != null && prev.has("updatedAt") && !prev.get("updatedAt").isJsonNull()
            ? prev.get("updatedAt").getAsString() : null;

        boolean contentChanged = prevSha == null || !prevSha.equals(sha256);

        if (!contentChanged && prevUpdatedAt != null) {
            // Хеш не змінився — НЕ перезаливаємо файл на R2, updatedAt лишається як був.
            wf.updatedAt = prevUpdatedAt;
            wf.transient_status = "skipped";
            log(onLog, "  = " + r2Key + " (без змін, пропущено)");
            return wf;
        }

        R2Client.UploadResult ur = r2.upload(localAbsPath, r2Key, null);
        if (!ur.success()) {
            warnings.add(r2Key + ": " + ur.error());
            wf.transient_status = "errored";
            log(onLog, "  ✗ " + r2Key + " — " + ur.error());
            return wf;
        }
        wf.updatedAt = Instant.now().toString();
        wf.transient_status = "uploaded";
        log(onLog, "  ✓ " + r2Key + (contentChanged ? " (оновлено)" : ""));
        return wf;
    }

    private static void log(java.util.function.Consumer<String> onLog, String msg) {
        if (onLog != null) onLog.accept(msg);
        log.info(msg);
    }

    private String safeEtag(String key) {
        try { return r2.head(key).etag(); } catch (Exception e) { return null; }
    }

    private Map<String, JsonObject> indexByPath(JsonObject manifest, String arrayField) {
        Map<String, JsonObject> result = new HashMap<>();
        if (manifest == null || !manifest.has(arrayField) || !manifest.get(arrayField).isJsonArray()) {
            return result;
        }
        for (JsonElement el : manifest.getAsJsonArray(arrayField)) {
            JsonObject obj = el.getAsJsonObject();
            if (obj.has("path") && !obj.get("path").isJsonNull()) {
                result.put(obj.get("path").getAsString(), obj);
            }
        }
        return result;
    }

    private String resolvePrevUpdatedAt(JsonObject manifest, String arrayField, String fileName) {
        if (manifest == null || !manifest.has(arrayField) || !manifest.get(arrayField).isJsonArray()) {
            return null;
        }
        for (JsonElement el : manifest.getAsJsonArray(arrayField)) {
            JsonObject obj = el.getAsJsonObject();
            if (obj.has("fileName") && fileName.equals(obj.get("fileName").getAsString())
                    && obj.has("updatedAt") && !obj.get("updatedAt").isJsonNull()) {
                return obj.get("updatedAt").getAsString();
            }
        }
        return null;
    }

    private String computeContentHash(PackManifestJson pm) {
        StringBuilder sb = new StringBuilder();
        sb.append(pm.mcVersion).append('|').append(pm.loader).append('|').append(pm.loaderVersion).append('|');
        for (HashModJson h : pm.hashMods) sb.append(h.fileName).append(':').append(h.sha1).append(';');
        for (WorkerFileJson w : pm.workerFiles) sb.append(w.path).append(':').append(w.sha256).append(';');
        for (OnMissingFileJson o : pm.onMissingFiles) sb.append(o.path).append(':').append(o.sha256).append(';');
        return sha256Hex(sb.toString().getBytes(StandardCharsets.UTF_8));
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

    private Path resolveMrpackPath(PackProject pack) {
        if (pack.getMrpackPath() != null && !pack.getMrpackPath().isBlank()) {
            return Path.of(pack.getMrpackPath());
        }
        return null;
    }

    private static String fileName(String path) {
        int slash = path.lastIndexOf('/');
        return slash >= 0 ? path.substring(slash + 1) : path;
    }

    private MrpackIndexFull parseFullIndex(Path mrpackPath) {
        if (mrpackPath == null || !Files.exists(mrpackPath)) return null;
        try (ZipInputStream zis = new ZipInputStream(Files.newInputStream(mrpackPath))) {
            java.util.zip.ZipEntry entry;
            while ((entry = zis.getNextEntry()) != null) {
                if ("modrinth.index.json".equals(entry.getName())) {
                    String json = new String(zis.readAllBytes(), StandardCharsets.UTF_8);
                    return GSON.fromJson(json, MrpackIndexFull.class);
                }
                zis.closeEntry();
            }
        } catch (IOException e) {
            log.error("Помилка читання mrpack index: {}", e.getMessage());
        }
        return null;
    }

    private JsonObject stripTransient(PackManifestJson pm) {
        JsonObject obj = GSON.toJsonTree(pm).getAsJsonObject();
        if (obj.has("workerFiles")) {
            for (JsonElement el : obj.getAsJsonArray("workerFiles")) {
                el.getAsJsonObject().remove("transient_status");
            }
        }
        return obj;
    }

    // ── JSON-моделі маніфесту (розділ 2.2 V2-документа) ────────────────────

    public static class PackManifestJson {
        public String id;
        public int schemaVersion;
        public String mcVersion, loader, loaderVersion;
        public String updatedAt;
        public String contentHash;
        public List<HashModJson> hashMods = new ArrayList<>();
        public List<WorkerFileJson> workerFiles = new ArrayList<>();
        public List<OnMissingFileJson> onMissingFiles = new ArrayList<>();
        public String serverIp;
        public String accentColor;
    }

    public static class HashModJson {
        public String fileName;
        public String sha1, sha512;
        public long fileSize;
        public String downloadUrl;
        public String updatedAt;
    }

    public static class WorkerFileJson {
        public String kind; // bundled-mod | override-file | override-folder
        public String fileName;   // для bundled-mod
        public String localPath;  // для override-file / override-folder
        public String path;       // R2-ключ
        public String sha256;
        public long fileSize;
        public long archiveSize;  // для override-folder
        public int fileCount;     // для override-folder
        public String updatedAt;
        // Службове поле, НЕ серіалізується (видаляється в stripTransient):
        // "uploaded" | "skipped" | "errored"
        public transient String transient_status;
    }

    public static class OnMissingFileJson {
        public String localPath;
        public String path;
        public String sha256;
    }

    // ── modrinth.index.json (повний, для генерації hashMods) ──────────────

    private static class MrpackIndexFull {
        List<MrpackFileFull> files;
    }

    private static class MrpackFileFull {
        String path;
        HashesFull hashes;
        Map<String, String> env;
        List<String> downloads;
        long fileSize;

        boolean isClientSide() {
            if (env == null) return true;
            String side = env.get("client");
            return side == null || !side.equals("unsupported");
        }
    }

    private static class HashesFull {
        String sha1, sha512;
    }
}
