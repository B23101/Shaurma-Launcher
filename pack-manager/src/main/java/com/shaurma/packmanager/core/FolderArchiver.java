package com.shaurma.packmanager.core;

import com.shaurma.packmanager.config.PackManagerConfig;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.*;
import java.nio.file.*;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.BiConsumer;
import java.util.zip.*;

/**
 * Архівація папок у .zip для завантаження на R2.
 *
 * Алгоритм (секція 9.3 специфікації):
 *  1. Рекурсивно обійти всі файли в папці (наприклад tacz/)
 *  2. Додати в ZipOutputStream з відносними шляхами БЕЗ префіксу папки
 *     Наприклад: tacz/guns/ak47.json → в архіві: guns/ak47.json
 *  3. Завантажити архів на R2 як archives/{packId}/{folderName}.zip
 *
 * При розпакуванні в лаунчері:
 *  - Завантажити архів у cache/
 *  - Розпакувати в packDir/{localPath}/
 *  - Шляхи в архіві додаються напряму до localPath
 */
public class FolderArchiver {

    private static final Logger log = LoggerFactory.getLogger(FolderArchiver.class);

    /**
     * Результат архівації.
     */
    public record ArchiveResult(
        Path archivePath,
        long archiveSize,
        long sourceSize,
        int fileCount,
        boolean success,
        String error
    ) {
        public static ArchiveResult ok(Path path, long archiveSize, long sourceSize, int count) {
            return new ArchiveResult(path, archiveSize, sourceSize, count, true, null);
        }
        public static ArchiveResult err(String error) {
            return new ArchiveResult(null, 0, 0, 0, false, error);
        }
    }

    /**
     * Заархівувати папку sourceDir в zip-файл outputZip.
     *
     * @param sourceDir  папка для архівації
     * @param outputZip  шлях до вихідного .zip файлу
     * @param onProgress прогрес (bytesProcessed, totalBytes)
     */
    public static ArchiveResult archive(
            Path sourceDir, Path outputZip, BiConsumer<Long, Long> onProgress) {

        if (!Files.isDirectory(sourceDir)) {
            return ArchiveResult.err("Директорія не існує: " + sourceDir);
        }

        int compressionLevel = PackManagerConfig.settings().zipCompressionLevel();

        try {
            // Підрахувати загальний розмір
            long totalSize = calcDirSize(sourceDir);
            AtomicLong processed = new AtomicLong(0);
            int[] fileCount = {0};

            Files.createDirectories(outputZip.getParent());

            try (OutputStream fos = new BufferedOutputStream(Files.newOutputStream(outputZip));
                 ZipOutputStream zos = new ZipOutputStream(fos)) {

                zos.setLevel(compressionLevel);

                // Обійти всі файли рекурсивно
                Files.walk(sourceDir)
                    .filter(p -> !Files.isDirectory(p))
                    .forEach(file -> {
                        try {
                            // Відносний шлях БЕЗ префіксу папки
                            String relative = sourceDir.relativize(file).toString()
                                .replace('\\', '/');

                            ZipEntry entry = new ZipEntry(relative);
                            entry.setLastModifiedTime(
                                java.nio.file.attribute.FileTime.fromMillis(
                                    Files.getLastModifiedTime(file).toMillis()));
                            zos.putNextEntry(entry);

                            byte[] buf = new byte[65536];
                            long fileSize = Files.size(file);
                            try (InputStream in = new BufferedInputStream(
                                    Files.newInputStream(file))) {
                                int r;
                                while ((r = in.read(buf)) != -1) {
                                    zos.write(buf, 0, r);
                                    long p = processed.addAndGet(r);
                                    if (onProgress != null) onProgress.accept(p, totalSize);
                                }
                            }

                            zos.closeEntry();
                            fileCount[0]++;

                        } catch (IOException e) {
                            log.error("Помилка архівації файлу {}: {}", file, e.getMessage());
                        }
                    });
            }

            long archiveSize = Files.size(outputZip);
            double ratio = totalSize > 0 ? (1.0 - (double) archiveSize / totalSize) * 100 : 0;

            log.info("Архів створено: {} ({} файлів, {} → {}, {:.1f}% стиснення)",
                outputZip.getFileName(), fileCount[0],
                formatBytes(totalSize), formatBytes(archiveSize), ratio);

            return ArchiveResult.ok(outputZip, archiveSize, totalSize, fileCount[0]);

        } catch (IOException e) {
            log.error("Помилка створення архіву: {}", e.getMessage());
            return ArchiveResult.err(e.getMessage());
        }
    }

    /**
     * Зручний метод — архівувати папку з налаштувань Pack Manager.
     *
     * @param packId     id збірки
     * @param folderName ім'я папки відносно dataDirectory (наприклад "tacz")
     */
    public static ArchiveResult archivePackFolder(
            String packId, String folderName, BiConsumer<Long, Long> onProgress) {

        String dataDir = PackManagerConfig.settings().dataDirectory();
        if (dataDir == null || dataDir.isBlank()) {
            return ArchiveResult.err("dataDirectory не налаштовано");
        }

        Path sourceDir = Path.of(dataDir, packId, folderName);
        Path outputZip = tempArchivePath(packId, folderName);

        return archive(sourceDir, outputZip, onProgress);
    }

    /**
     * Тимчасовий шлях для архіву.
     */
    public static Path tempArchivePath(String packId, String folderName) {
        String cacheDir = PackManagerConfig.settings().cacheDirectory();
        Path cacheBase = (cacheDir != null && !cacheDir.isBlank())
            ? Path.of(cacheDir)
            : Path.of(System.getProperty("java.io.tmpdir"), "pack-manager-cache");
        return cacheBase.resolve(packId + "_" + folderName + ".zip");
    }

    /**
     * R2 ключ для архіву папки.
     * archives/{packId}/{folderName}.zip
     */
    public static String archiveR2Key(String packId, String folderName) {
        String cleanName = folderName.replace("/", "").replace("\\", "");
        return "archives/" + packId + "/" + cleanName + ".zip";
    }

    private static long calcDirSize(Path dir) throws IOException {
        try (var s = Files.walk(dir)) {
            return s.filter(p -> !Files.isDirectory(p))
                    .mapToLong(p -> { try { return Files.size(p); } catch (IOException e) { return 0; } })
                    .sum();
        }
    }

    private static String formatBytes(long b) {
        if (b < 1024)       return b + " B";
        if (b < 1048576)    return String.format("%.1f KB", b / 1024.0);
        if (b < 1073741824) return String.format("%.1f MB", b / 1048576.0);
        return String.format("%.2f GB", b / 1073741824.0);
    }
}
