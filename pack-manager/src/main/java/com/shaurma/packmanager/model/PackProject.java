package com.shaurma.packmanager.model;

import com.google.gson.annotations.SerializedName;
import java.util.*;

/**
 * Внутрішня модель збірки в Pack Manager.
 * Окремо від ServerManifest — тут зберігаються дані для редагування.
 */
public class PackProject {

    // ── Основні дані збірки ───────────────────────────────────────────────

    private String id;
    private String name;
    @SerializedName("mcVersion")     private String mcVersion;
    private String loader;           // FABRIC, FORGE, NEOFORGE, QUILT
    @SerializedName("loaderVersion") private String loaderVersion;
    @SerializedName("mrpackUrl")     private String mrpackUrl;
    @SerializedName("mrpackPath")    private String mrpackPath;  // Локальний шлях до mrpack
    @SerializedName("iconPath")      private String iconPath;      // Локальний шлях до icon.png
    @SerializedName("backgroundPath") private String backgroundPath; // Локальний шлях до background.jpg
    private List<String> tags;
    private String description;
    @SerializedName("serverIp")
    private String serverIp;
    @SerializedName("accentColor")
    private String accentColor;

    // ── Правила синхронізації ─────────────────────────────────────────────
    /** Карта: шлях → правило (hash, force_archive, on_missing, first_install_only, skip) */
    @SerializedName("syncRules")     private Map<String, String> syncRules;

    // ── Стан публікації ───────────────────────────────────────────────────
    @SerializedName("lastDeployed")  private String lastDeployed;
    @SerializedName("isDirty")       private boolean isDirty;  // Є незбережені зміни

    // ── Статистика (заповнюється при генерації маніфесту) ─────────────────
    private transient int hashModCount;
    private transient int bundledModCount;
    private transient long totalSize;

    // ── Конструктор ───────────────────────────────────────────────────────

    public PackProject() {
        this.syncRules = new LinkedHashMap<>(defaultSyncRules());
        this.tags      = new ArrayList<>();
        this.isDirty   = true;
    }

    public static PackProject createNew(String id, String name) {
        PackProject p = new PackProject();
        p.id   = id;
        p.name = name;
        return p;
    }

    // ── Правила за замовчуванням (з специфікації) ─────────────────────────

    public static Map<String, String> defaultSyncRules() {
        Map<String, String> rules = new LinkedHashMap<>();
        rules.put("mods/",          "hash");
        rules.put("tacz/",          "force_archive");
        rules.put("config/",        "on_missing");
        rules.put("resourcepacks/", "first_install_only");
        rules.put("shaderpacks/",   "first_install_only");
        rules.put("options.txt",    "on_missing");
        rules.put("servers.dat",    "on_missing");
        rules.put("saves/",         "skip");
        rules.put("screenshots/",   "skip");
        rules.put("logs/",          "skip");
        return rules;
    }

    // ── Sync rule constants ───────────────────────────────────────────────

    public static final String RULE_HASH               = "hash";
    public static final String RULE_FORCE_ARCHIVE      = "force_archive";
    public static final String RULE_ON_MISSING         = "on_missing";
    public static final String RULE_FIRST_INSTALL_ONLY = "first_install_only";
    public static final String RULE_SKIP               = "skip";

    public static final String[] ALL_RULES = {
        RULE_HASH, RULE_FORCE_ARCHIVE, RULE_ON_MISSING, RULE_FIRST_INSTALL_ONLY, RULE_SKIP
    };

    /** Опис правила синхронізації для UI */
    public static String ruleDescription(String rule) {
        return switch (rule) {
            case RULE_HASH ->
                "hash — Порівняти SHA1/SHA512. Змінився — завантажити. Зник — видалити.";
            case RULE_FORCE_ARCHIVE ->
                "force_archive — Зберігається як .zip на R2. HEAD → розмір. " +
                "Якщо змінився — завантажити і розпакувати поверх.";
            case RULE_ON_MISSING ->
                "on_missing — Копіювати ТІЛЬКИ якщо файл відсутній. " +
                "Існуючі файли не чіпати (гравець міг змінити).";
            case RULE_FIRST_INSTALL_ONLY ->
                "first_install_only — Встановити один раз при першому встановленні. " +
                "Після — ніколи не оновлювати.";
            case RULE_SKIP ->
                "skip — НІКОЛИ не чіпати. Ігрові світи, скріни тощо.";
            default -> rule;
        };
    }

    /** CSS клас для значка правила */
    public static String ruleStyleClass(String rule) {
        return switch (rule) {
            case RULE_HASH               -> "badge-hash";
            case RULE_FORCE_ARCHIVE      -> "badge-archive";
            case RULE_ON_MISSING         -> "badge-missing";
            case RULE_FIRST_INSTALL_ONLY -> "badge-first";
            case RULE_SKIP               -> "badge-skip";
            default                      -> "badge-unknown";
        };
    }

    // ── Getters/Setters ───────────────────────────────────────────────────

    public String getId()            { return id; }
    public void setId(String id)     { this.id = id; markDirty(); }

    public String getName()          { return name; }
    public void setName(String name) { this.name = name; markDirty(); }

    public String getMcVersion()              { return mcVersion; }
    public void setMcVersion(String v)        { this.mcVersion = v; markDirty(); }

    public String getLoader()                 { return loader; }
    public void setLoader(String loader)      { this.loader = loader; markDirty(); }

    public String getLoaderVersion()          { return loaderVersion; }
    public void setLoaderVersion(String v)    { this.loaderVersion = v; markDirty(); }

    public String getMrpackUrl()              { return mrpackUrl; }
    public void setMrpackUrl(String url)      { this.mrpackUrl = url; markDirty(); }

    public String getMrpackPath()             { return mrpackPath; }
    public void setMrpackPath(String path)    { this.mrpackPath = path; markDirty(); }

    public String getIconPath()               { return iconPath; }
    public void setIconPath(String path)      { this.iconPath = path; markDirty(); }

    public String getBackgroundPath()         { return backgroundPath; }
    public void setBackgroundPath(String path){ this.backgroundPath = path; markDirty(); }

    public List<String> getTags()             { return tags; }
    public void setTags(List<String> tags)    { this.tags = tags; markDirty(); }

    public String getDescription()            { return description; }
    public void setDescription(String d)      { this.description = d; markDirty(); }

    public String getServerIp()                { return serverIp; }
    public void setServerIp(String ip)         { this.serverIp = ip; markDirty(); }

    public String getAccentColor()              { return accentColor; }
    public void setAccentColor(String color)    { this.accentColor = color; markDirty(); }

    public Map<String, String> getSyncRules() { return syncRules; }
    public void setSyncRule(String path, String rule) {
        syncRules.put(path, rule); markDirty();
    }
    public void removeSyncRule(String path) {
        syncRules.remove(path); markDirty();
    }

    public String getLastDeployed()           { return lastDeployed; }
    public void setLastDeployed(String d)     { this.lastDeployed = d; }

    public boolean isDirty()                  { return isDirty; }
    public void markDirty()                   { this.isDirty = true; }
    public void markClean()                   { this.isDirty = false; }

    public int getHashModCount()              { return hashModCount; }
    public void setHashModCount(int c)        { this.hashModCount = c; }

    public int getBundledModCount()           { return bundledModCount; }
    public void setBundledModCount(int c)     { this.bundledModCount = c; }

    public long getTotalSize()                { return totalSize; }
    public void setTotalSize(long s)          { this.totalSize = s; }

    @Override
    public String toString() {
        return "PackProject{id='" + id + "', name='" + name + "'}";
    }
}
