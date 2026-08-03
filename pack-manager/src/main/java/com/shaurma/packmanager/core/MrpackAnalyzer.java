package com.shaurma.packmanager.core;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.zip.ZipEntry;
import java.util.zip.ZipInputStream;

/**
 * Аналізує .mrpack (modrinth.index.json) і визначає mcVersion / loader /
 * loaderVersion автоматично, без ручного вибору в UI.
 *
 * modrinth.index.json завжди містить блок "dependencies", наприклад:
 * {
 *   "dependencies": {
 *     "minecraft": "1.20.1",
 *     "fabric-loader": "0.15.11"
 *   }
 * }
 *
 * Ключ лоадера однозначно визначає сам лоадер:
 *   fabric-loader  -> FABRIC
 *   forge          -> FORGE
 *   neoforge       -> NEOFORGE
 *   quilt-loader   -> QUILT
 */
public final class MrpackAnalyzer {

    private static final Logger log = LoggerFactory.getLogger(MrpackAnalyzer.class);

    private MrpackAnalyzer() {}

    public record AnalysisResult(
        String mcVersion,
        String loader,
        String loaderVersion,
        boolean success,
        String error
    ) {
        public static AnalysisResult error(String msg) {
            return new AnalysisResult(null, null, null, false, msg);
        }
    }

    private static final java.util.Map<String, String> LOADER_KEYS = java.util.Map.of(
        "fabric-loader", "FABRIC",
        "forge",         "FORGE",
        "neoforge",      "NEOFORGE",
        "quilt-loader",  "QUILT"
    );

    /**
     * Аналізує .mrpack файл і повертає mcVersion/loader/loaderVersion,
     * визначені з modrinth.index.json -> dependencies.
     */
    public static AnalysisResult analyze(Path mrpackFile) {
        if (mrpackFile == null || !Files.exists(mrpackFile)) {
            return AnalysisResult.error("Файл .mrpack не знайдено: " + mrpackFile);
        }

        try (ZipInputStream zis = new ZipInputStream(Files.newInputStream(mrpackFile))) {
            ZipEntry entry;
            while ((entry = zis.getNextEntry()) != null) {
                if (!"modrinth.index.json".equals(entry.getName())) {
                    zis.closeEntry();
                    continue;
                }

                String json = new String(zis.readAllBytes(), StandardCharsets.UTF_8);
                JsonObject root = JsonParser.parseString(json).getAsJsonObject();

                if (!root.has("dependencies") || !root.get("dependencies").isJsonObject()) {
                    return AnalysisResult.error(
                        "modrinth.index.json не містить блоку \"dependencies\"");
                }

                JsonObject deps = root.getAsJsonObject("dependencies");

                String mcVersion = deps.has("minecraft") && !deps.get("minecraft").isJsonNull()
                    ? deps.get("minecraft").getAsString() : null;

                String loader = null;
                String loaderVersion = null;
                for (var e : LOADER_KEYS.entrySet()) {
                    if (deps.has(e.getKey()) && !deps.get(e.getKey()).isJsonNull()) {
                        loader = e.getValue();
                        loaderVersion = deps.get(e.getKey()).getAsString();
                        break;
                    }
                }

                if (mcVersion == null) {
                    return AnalysisResult.error(
                        "modrinth.index.json: не знайдено \"minecraft\" в dependencies");
                }
                if (loader == null) {
                    log.warn("[MrpackAnalyzer] Лоадер не визначено для {} (dependencies={})",
                        mrpackFile.getFileName(), deps);
                }

                return new AnalysisResult(mcVersion, loader, loaderVersion, true, null);
            }
        } catch (IOException e) {
            return AnalysisResult.error("Помилка читання .mrpack: " + e.getMessage());
        }

        return AnalysisResult.error("modrinth.index.json не знайдено в .mrpack");
    }
}
