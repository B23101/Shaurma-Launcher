package com.shaurma.packmanager.config;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.annotations.SerializedName;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * Конфігурація Pack Manager.
 * Зберігається у pack-manager-settings.json в папці з даними (або поряд з .exe).
 */
public final class PackManagerConfig {

    private static final Logger log = LoggerFactory.getLogger(PackManagerConfig.class);
    private static final Gson   GSON = new GsonBuilder().setPrettyPrinting().create();

    private static Path exeDir;
    private static Path settingsFile;
    private static Settings settings;

    private PackManagerConfig() {}

    public static void init() throws IOException {
        exeDir = Path.of(System.getProperty("user.dir"));
        Path exeSettings = exeDir.resolve("pack-manager-settings.json");

        // Спочатку читаємо з exe-папки (початкове завантаження)
        if (Files.exists(exeSettings)) {
            try {
                settings = GSON.fromJson(Files.readString(exeSettings), Settings.class);
                if (settings == null) settings = Settings.defaults();
            } catch (Exception e) {
                log.warn("Не вдалось прочитати налаштування: {}", e.getMessage());
                settings = Settings.defaults();
            }
        } else {
            settings = Settings.defaults();
        }

        // Якщо dataDirectory вказана — намагаємось читати звідти (вона пріоритетніша)
        if (settings.dataDirectory() != null && !settings.dataDirectory().isBlank()) {
            Path dataDir = Path.of(settings.dataDirectory());
            Path dataSettings = dataDir.resolve("pack-manager-settings.json");
            if (Files.exists(dataSettings)) {
                try {
                    Settings dataS = GSON.fromJson(Files.readString(dataSettings), Settings.class);
                    if (dataS != null) settings = dataS;
                } catch (Exception e) {
                    log.warn("Не вдалось прочитати налаштування з dataDirectory: {}", e.getMessage());
                }
            }
            settingsFile = dataSettings;
            Files.createDirectories(dataDir);
        } else {
            settingsFile = exeSettings;
        }

        // Зберігаємо якщо файлу ще нема
        if (!Files.exists(settingsFile)) {
            save();
        }

        log.info("Pack Manager ініціалізовано. Settings: {}", settingsFile);
        log.info("R2 publicUrl prefix: {}", settings.r2().publicUrlBase());
    }

    /** Робоча папка — dataDirectory якщо вказана, інакше exe-папка */
    public static Path workDir() {
        if (settings != null && settings.dataDirectory() != null && !settings.dataDirectory().isBlank()) {
            return Path.of(settings.dataDirectory());
        }
        return exeDir;
    }

    public static Path settingsFile() { return settingsFile; }
    public static Settings settings() { return settings; }

    public static void updateSettings(Settings newSettings) throws IOException {
        settings = newSettings;
        save();
    }

    private static void save() throws IOException {
        Files.writeString(settingsFile, GSON.toJson(settings));
        // Дублюємо в exe-папку для сумісності
        Path exeSettings = exeDir.resolve("pack-manager-settings.json");
        if (!exeSettings.equals(settingsFile)) {
            Files.writeString(exeSettings, GSON.toJson(settings));
        }
    }

    // ── Модель налаштувань ────────────────────────────────────────────────

    /**
     * Налаштування Pack Manager.
     */
    public record Settings(
        /** Шлях до папки з даними сервера (де лежать .mrpack і папки збірок) */
        @SerializedName("dataDirectory")     String dataDirectory,

        /** Шлях до кешу (тимчасові файли) */
        @SerializedName("cacheDirectory")    String cacheDirectory,

        /** Cloudflare R2 налаштування */
        @SerializedName("r2")                R2Settings r2,

        /** Папки що автоматично архівуються (force_archive) */
        @SerializedName("archiveFolders")    java.util.List<String> archiveFolders,

        /** Рівень стиснення ZIP (1-9) */
        @SerializedName("zipCompressionLevel") int zipCompressionLevel,

        /** Автоматично генерувати manifest після завантаження mrpack */
        @SerializedName("autoGenerateManifest") boolean autoGenerateManifest
    ) {
        public static Settings defaults() {
            return new Settings(
                "",
                "",
                R2Settings.empty(),
                java.util.List.of("tacz/"),
                6,
                true
            );
        }

        public Settings withDataDirectory(String dir) {
            return new Settings(dir, cacheDirectory, r2, archiveFolders,
                zipCompressionLevel, autoGenerateManifest);
        }

        public Settings withR2(R2Settings newR2) {
            return new Settings(dataDirectory, cacheDirectory, newR2, archiveFolders,
                zipCompressionLevel, autoGenerateManifest);
        }
    }

    /**
     * Налаштування підключення до Cloudflare R2.
     */
    public record R2Settings(
        @SerializedName("accountId")       String accountId,
        @SerializedName("accessKeyId")     String accessKeyId,
        @SerializedName("secretAccessKey") String secretAccessKey,
        @SerializedName("bucketName")      String bucketName,
        @SerializedName("publicDomain")    String publicDomain,
        @SerializedName("endpoint")        String endpoint
    ) {
        public static R2Settings empty() {
            return new R2Settings("", "", "", "", "", "");
        }

        public boolean isConfigured() {
            return accountId != null && !accountId.isBlank()
                && accessKeyId != null && !accessKeyId.isBlank()
                && secretAccessKey != null && !secretAccessKey.isBlank()
                && bucketName != null && !bucketName.isBlank();
        }

        /** R2 S3-endpoint URL */
        public String s3Endpoint() {
            if (endpoint != null && !endpoint.isBlank()) return endpoint;
            return "https://" + accountId + ".r2.cloudflarestorage.com";
        }

        /**
         * Базовий публічний CDN URL (без слешу на кінці).
         *
         * Пріоритет:
         *   1. publicDomain якщо явно вказано (кастомний домен)
         *   2. Стандартний r2.dev URL: https://{bucketName}.{accountId}.r2.dev
         *
         * ВАЖЛИВО: лаунчер (LauncherConfig.SERVER_BASE_URL) МАЄ збігатись
         * з цим значенням. Якщо publicDomain змінюється — оновити також і лаунчер.
         */
        public String publicUrlBase() {
            if (publicDomain != null && !publicDomain.isBlank()) {
                return publicDomain.replaceAll("/$", "");
            }
            // Стандартний Cloudflare R2 публічний URL
            return "https://" + bucketName + "." + accountId + ".r2.dev";
        }

        /** Публічний CDN URL для конкретного ключа */
        public String publicUrl(String key) {
            return publicUrlBase() + "/" + key;
        }
    }
}
