package com.shaurma.packmanager.core;

import com.shaurma.packmanager.config.PackManagerConfig;
import com.shaurma.packmanager.model.PackProject;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.function.Consumer;

/**
 * DeployService V2 (DOWNLOAD_SYNC_DESIGN_V2.md).
 *
 * Скасовує підхід "заливаємо .mrpack цілим файлом" — натомість:
 *   1. Розпаковуємо .mrpack ЛОКАЛЬНО (завжди, для кожної збірки).
 *   2. Класифікуємо вміст (hashMods / bundled-моди / override-файли /
 *      override-теки) через MrpackExtractor.
 *   3. Заливаємо кожен елемент окремо, порівнюючи SHA256 з попереднім
 *      manifest.json — перезаливаємо тільки реально змінені файли.
 *   4. Генеруємо packs/<id>/manifest.json (ManifestGeneratorV2) і
 *      оновлюємо ТІЛЬКИ запис цієї збірки в packs-index.json.
 *
 * .mrpack як цілісний файл ПІСЛЯ деплою на R2 більше не існує і launcher-ом
 * ніколи не якається.
 */
public class DeployServiceV2 {

    private static final Logger log = LoggerFactory.getLogger(DeployServiceV2.class);

    // Рівень логування події деплою. Колись жив у V1-класі DeployService
    // (звідси ім'я); V1 видалено, enum переїхав сюди — єдиний власник.
    public enum LogLevel { INFO, WARN, ERROR, SUCCESS }

    public sealed interface DeployEvent
        permits DeployEvent.Step, DeployEvent.Log, DeployEvent.Done, DeployEvent.Error {
        record Step(String name, int stepNum, int totalSteps) implements DeployEvent {}
        record Log(String message, LogLevel level) implements DeployEvent {}
        record Done(DeployReportV2 report) implements DeployEvent {}
        record Error(String message) implements DeployEvent {}
    }

    public record DeployReportV2(
        int packsDeployed,
        int filesUploaded,
        int filesSkipped,
        int filesErrored,
        long durationMs,
        List<String> warnings
    ) {}

    public CompletableFuture<DeployReportV2> deploy(List<PackProject> packs, Consumer<DeployEvent> onEvent) {
        return CompletableFuture.supplyAsync(() -> {
            long startMs = System.currentTimeMillis();
            List<String> allWarnings = new ArrayList<>();
            int packsDeployed = 0, totalUploaded = 0, totalSkipped = 0, totalErrored = 0;

            PackManagerConfig.R2Settings cfg = PackManagerConfig.settings().r2();
            if (!cfg.isConfigured()) {
                emit(onEvent, new DeployEvent.Error("R2 не налаштовано"));
                return new DeployReportV2(0, 0, 0, 1, 0, List.of("R2 не налаштовано"));
            }

            try (R2Client r2 = new R2Client(cfg)) {
                ManifestGeneratorV2 generator = new ManifestGeneratorV2(r2);
                int step = 0;
                int totalSteps = packs.size() * 4; // extract, upload icon/bg, manifest, index

                for (PackProject pack : packs) {
                    emit(onEvent, new DeployEvent.Log(
                        "=== Деплой (V2): " + pack.getName() + " ===", LogLevel.INFO));

                    Path mrpackPath = pack.getMrpackPath() != null && !pack.getMrpackPath().isBlank()
                        ? Path.of(pack.getMrpackPath()) : null;
                    if (mrpackPath == null || !Files.exists(mrpackPath)) {
                        emit(onEvent, new DeployEvent.Log(
                            "[" + pack.getId() + "] .mrpack не знайдено, пропускаємо: " + pack.getMrpackPath(),
                            LogLevel.WARN));
                        allWarnings.add("[" + pack.getId() + "] mrpack не знайдено");
                        continue;
                    }

                    // ── Крок 1: розпакувати .mrpack локально ────────────────
                    step++;
                    emit(onEvent, new DeployEvent.Step(
                        "Розпаковка .mrpack: " + pack.getId(), step, totalSteps));

                    MrpackExtractor.ExtractionResult extraction;
                    try {
                        extraction = MrpackExtractor.extract(mrpackPath, pack.getId());
                    } catch (IOException e) {
                        emit(onEvent, new DeployEvent.Log(
                            "[" + pack.getId() + "] Помилка розпаковки: " + e.getMessage(),
                            LogLevel.ERROR));
                        allWarnings.add("[" + pack.getId() + "] extract failed: " + e.getMessage());
                        totalErrored++;
                        continue;
                    }

                    try {
                        // ── Крок 2: icon / background (як і раніше) ─────────
                        step++;
                        emit(onEvent, new DeployEvent.Step(
                            "Upload icon/background: " + pack.getId(), step, totalSteps));
                        uploadIconAndBackground(r2, pack, onEvent);

                        // ── Крок 3: генерація manifest.json + upload файлів ──
                        step++;
                        emit(onEvent, new DeployEvent.Step(
                            "Генерація manifest.json: " + pack.getId(), step, totalSteps));

                        final int[] counters = {0, 0, 0}; // uploaded, skipped, errored
                        ManifestGeneratorV2.GenerationResult genResult = generator.deployPack(
                            pack, extraction,
                            line -> {
                                emit(onEvent, new DeployEvent.Log(line, LogLevel.INFO));
                                if (line.startsWith("  ✓")) counters[0]++;
                                else if (line.startsWith("  =")) counters[1]++;
                                else if (line.startsWith("  ✗")) counters[2]++;
                            });

                        totalUploaded += counters[0];
                        totalSkipped += counters[1];
                        totalErrored += counters[2];

                        if (!genResult.success() || genResult.manifest() == null) {
                            allWarnings.addAll(genResult.warnings());
                            emit(onEvent, new DeployEvent.Log(
                                "[" + pack.getId() + "] Генерація manifest.json завершилась з помилками",
                                LogLevel.ERROR));
                            continue;
                        }
                        allWarnings.addAll(genResult.warnings());

                        // ── Крок 4: оновити ТІЛЬКИ цю збірку в packs-index.json ─
                        step++;
                        emit(onEvent, new DeployEvent.Step(
                            "Оновлення packs-index.json: " + pack.getId(), step, totalSteps));
                        List<String> indexWarnings = generator.updatePacksIndex(pack, genResult.manifest());
                        allWarnings.addAll(indexWarnings);

                        pack.setLastDeployed(Instant.now().toString());
                        pack.markClean();
                        packsDeployed++;

                        emit(onEvent, new DeployEvent.Log(
                            "✓ Деплой завершено: " + pack.getId(), LogLevel.SUCCESS));

                    } finally {
                        MrpackExtractor.cleanup(extraction.extractedRoot());
                    }
                }

                long durationMs = System.currentTimeMillis() - startMs;
                DeployReportV2 report = new DeployReportV2(
                    packsDeployed, totalUploaded, totalSkipped, totalErrored, durationMs, allWarnings);

                emit(onEvent, new DeployEvent.Log(
                    String.format("Деплой V2 завершено за %.1f сек. Збірок: %d, залито: %d, без змін: %d, помилок: %d",
                        durationMs / 1000.0, packsDeployed, totalUploaded, totalSkipped, totalErrored),
                    totalErrored > 0 ? LogLevel.WARN : LogLevel.SUCCESS));
                emit(onEvent, new DeployEvent.Done(report));
                return report;

            } catch (Exception e) {
                log.error("Deploy V2 failed", e);
                emit(onEvent, new DeployEvent.Error(e.getMessage()));
                return new DeployReportV2(0, 0, 0, 1, System.currentTimeMillis() - startMs,
                    List.of(e.getMessage()));
            }
        });
    }

    private void uploadIconAndBackground(R2Client r2, PackProject pack, Consumer<DeployEvent> onEvent) {
        if (pack.getIconPath() != null && !pack.getIconPath().isBlank()) {
            Path iconFile = Path.of(pack.getIconPath());
            if (Files.exists(iconFile)) {
                String iconKey = "packs/" + pack.getId() + "/icon.png";
                R2Client.UploadResult ur = r2.upload(iconFile, iconKey, null);
                emit(onEvent, new DeployEvent.Log(
                    ur.success() ? "✓ " + iconKey : "✗ " + iconKey + ": " + ur.error(),
                    ur.success() ? LogLevel.SUCCESS : LogLevel.ERROR));
            }
        }
        if (pack.getBackgroundPath() != null && !pack.getBackgroundPath().isBlank()) {
            Path bgFile = Path.of(pack.getBackgroundPath());
            if (Files.exists(bgFile)) {
                String bgName = bgFile.getFileName().toString();
                String ext = bgName.contains(".") ? bgName.substring(bgName.lastIndexOf('.') + 1) : "jpg";
                String bgKey = "packs/" + pack.getId() + "/background." + ext;
                R2Client.UploadResult ur = r2.upload(bgFile, bgKey, null);
                emit(onEvent, new DeployEvent.Log(
                    ur.success() ? "✓ " + bgKey : "✗ " + bgKey + ": " + ur.error(),
                    ur.success() ? LogLevel.SUCCESS : LogLevel.ERROR));
            }
        }
    }

    private static void emit(Consumer<DeployEvent> onEvent, DeployEvent event) {
        if (onEvent != null) onEvent.accept(event);
    }
}
