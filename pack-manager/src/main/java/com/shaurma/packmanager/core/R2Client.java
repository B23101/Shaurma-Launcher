package com.shaurma.packmanager.core;

import com.shaurma.packmanager.config.PackManagerConfig;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import software.amazon.awssdk.auth.credentials.AwsBasicCredentials;
import software.amazon.awssdk.auth.credentials.StaticCredentialsProvider;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.regions.Region;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.*;
import software.amazon.awssdk.services.s3.presigner.S3Presigner;

import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.function.BiConsumer;
import java.util.function.Consumer;
import java.util.function.LongConsumer;

/**
 * Клієнт для взаємодії з Cloudflare R2 через AWS SDK v2 (S3-сумісний API).
 *
 * Підтримує:
 *  - upload файлів з прогресом
 *  - head (перевірка існування, розмір, ETag)
 *  - список файлів в bucket
 *  - видалення файлів
 *  - перевірка з'єднання
 */
public class R2Client implements AutoCloseable {

    private static final Logger log = LoggerFactory.getLogger(R2Client.class);

    private final S3Client s3;
    private final String   bucket;
    private final PackManagerConfig.R2Settings cfg;

    public R2Client(PackManagerConfig.R2Settings cfg) {
        this.cfg    = cfg;
        this.bucket = cfg.bucketName();
        this.s3 = S3Client.builder()
            .endpointOverride(URI.create(cfg.s3Endpoint()))
            .region(Region.of("auto"))
            .credentialsProvider(StaticCredentialsProvider.create(
                AwsBasicCredentials.create(cfg.accessKeyId(), cfg.secretAccessKey())
            ))
            .forcePathStyle(true)
            .build();
    }

    // ── Результати ────────────────────────────────────────────────────────

    public record HeadResult(String etag, long size, String lastModified) {}

    public record ListEntry(String key, long size, String lastModified) {}

    public record UploadResult(String key, boolean success, String error) {
        public static UploadResult ok(String key) { return new UploadResult(key, true, null); }
        public static UploadResult err(String key, String e) { return new UploadResult(key, false, e); }
    }

    // ── Операції ──────────────────────────────────────────────────────────

    /**
     * Завантажити файл на R2.
     * @param localPath  локальний файл
     * @param key        ключ на R2 (шлях в bucket)
     * @param onProgress прогрес (bytesUploaded, totalBytes)
     */
    public UploadResult upload(Path localPath, String key, BiConsumer<Long, Long> onProgress) {
        try {
            long fileSize = java.nio.file.Files.size(localPath);
            log.info("Upload: {} → {} ({})", localPath.getFileName(), key, formatBytes(fileSize));

            PutObjectRequest req = PutObjectRequest.builder()
                .bucket(bucket)
                .key(key)
                .contentLength(fileSize)
                .build();

            if (onProgress != null) {
                // Simulated progress (SDK не підтримує прогрес для sync клієнта)
                onProgress.accept(0L, fileSize);
            }

            s3.putObject(req, RequestBody.fromFile(localPath));

            if (onProgress != null) onProgress.accept(fileSize, fileSize);
            log.info("Upload OK: {}", key);
            return UploadResult.ok(key);

        } catch (Exception e) {
            log.error("Upload failed: {} → {}: {}", localPath, key, e.getMessage());
            return UploadResult.err(key, e.getMessage());
        }
    }

    /**
     * HEAD запит — отримати ETag і розмір файлу без завантаження.
     */
    public HeadResult head(String key) {
        try {
            HeadObjectRequest req = HeadObjectRequest.builder()
                .bucket(bucket)
                .key(key)
                .build();
            HeadObjectResponse resp = s3.headObject(req);
            return new HeadResult(resp.eTag(), resp.contentLength(), resp.lastModified().toString());
        } catch (NoSuchKeyException e) {
            throw new RuntimeException("Файл не знайдено: " + key, e);
        } catch (Exception e) {
            throw new RuntimeException("HEAD failed: " + key + ": " + e.getMessage(), e);
        }
    }

    /**
     * Чи існує файл на R2.
     */
    public boolean exists(String key) {
        try {
            head(key);
            return true;
        } catch (Exception e) {
            return false;
        }
    }

    /**
     * Список файлів в bucket (з опціональним префіксом).
     */
    public List<ListEntry> list(String prefix) {
        List<ListEntry> result = new ArrayList<>();
        try {
            ListObjectsV2Request.Builder reqBuilder = ListObjectsV2Request.builder()
                .bucket(bucket);
            if (prefix != null && !prefix.isBlank()) {
                reqBuilder.prefix(prefix);
            }

            ListObjectsV2Response resp;
            String token = null;
            do {
                if (token != null) reqBuilder.continuationToken(token);
                resp = s3.listObjectsV2(reqBuilder.build());
                for (S3Object obj : resp.contents()) {
                    result.add(new ListEntry(
                        obj.key(),
                        obj.size(),
                        obj.lastModified().toString()
                    ));
                }
                token = resp.nextContinuationToken();
            } while (resp.isTruncated());

        } catch (Exception e) {
            log.error("List failed: {}", e.getMessage());
        }
        return result;
    }

    /**
     * Видалити файл з R2.
     */
    public boolean delete(String key) {
        try {
            s3.deleteObject(DeleteObjectRequest.builder()
                .bucket(bucket)
                .key(key)
                .build());
            log.info("Deleted: {}", key);
            return true;
        } catch (Exception e) {
            log.error("Delete failed: {}: {}", key, e.getMessage());
            return false;
        }
    }

    /**
     * Завантажити текстовий вміст файлу з R2 (UTF-8).
     * @return вміст або null якщо файл не існує
     */
    public String getString(String key) {
        try {
            GetObjectRequest req = GetObjectRequest.builder()
                .bucket(bucket).key(key).build();
            byte[] bytes = s3.getObjectAsBytes(req).asByteArray();
            log.debug("Downloaded: {} ({} bytes)", key, bytes.length);
            return new String(bytes, StandardCharsets.UTF_8);
        } catch (NoSuchKeyException e) {
            return null;
        } catch (Exception e) {
            log.error("getString failed: {}: {}", key, e.getMessage());
            return null;
        }
    }

    /**
     * Завантажити бінарний вміст файлу з R2.
     * @return bytes або null якщо файл не існує
     */
    public byte[] getBytes(String key) {
        try {
            GetObjectRequest req = GetObjectRequest.builder()
                .bucket(bucket).key(key).build();
            return s3.getObjectAsBytes(req).asByteArray();
        } catch (NoSuchKeyException e) {
            return null;
        } catch (Exception e) {
            log.error("getBytes failed: {}: {}", key, e.getMessage());
            return null;
        }
    }

    /**
     * Завантажити рядок на R2 як UTF-8 текст.
     */
    public UploadResult putString(String key, String content, String contentType) {
        try {
            byte[] bytes = content.getBytes(StandardCharsets.UTF_8);
            PutObjectRequest req = PutObjectRequest.builder()
                .bucket(bucket).key(key)
                .contentType(contentType != null ? contentType : "application/json")
                .contentLength((long) bytes.length)
                .build();
            s3.putObject(req, RequestBody.fromBytes(bytes));
            log.info("putString: {} ({} bytes)", key, bytes.length);
            return new UploadResult(key, true, null);
        } catch (Exception e) {
            log.error("putString failed: {}: {}", key, e.getMessage());
            return UploadResult.err(key, e.getMessage());
        }
    }

    /**
     * Завантажити бінарні дані на R2.
     */
    public UploadResult putBytes(String key, byte[] bytes, String contentType) {
        try {
            PutObjectRequest req = PutObjectRequest.builder()
                .bucket(bucket).key(key)
                .contentType(contentType != null ? contentType : "application/octet-stream")
                .contentLength((long) bytes.length)
                .build();
            s3.putObject(req, RequestBody.fromBytes(bytes));
            log.info("putBytes: {} ({} bytes)", key, bytes.length);
            return new UploadResult(key, true, null);
        } catch (Exception e) {
            log.error("putBytes failed: {}: {}", key, e.getMessage());
            return UploadResult.err(key, e.getMessage());
        }
    }

    /**
     * Перевірити з'єднання з R2 (тестовий запит).
     * @return null якщо OK, або рядок з помилкою
     */
    public String testConnection() {
        try {
            s3.listObjectsV2(ListObjectsV2Request.builder()
                .bucket(bucket)
                .maxKeys(1)
                .build());
            return null; // OK
        } catch (Exception e) {
            return e.getMessage();
        }
    }

    @Override
    public void close() {
        try { s3.close(); } catch (Exception ignored) {}
    }

    // ── Фабричний метод ───────────────────────────────────────────────────

    /**
     * Створити R2Client з поточних налаштувань.
     * @throws IllegalStateException якщо R2 не налаштовано
     */
    public static R2Client fromConfig() {
        PackManagerConfig.R2Settings cfg = PackManagerConfig.settings().r2();
        if (!cfg.isConfigured()) {
            throw new IllegalStateException(
                "R2 не налаштовано. Заповніть Account ID, Access Key, Secret та Bucket Name.");
        }
        return new R2Client(cfg);
    }

    private static String formatBytes(long b) {
        if (b < 1024)       return b + " B";
        if (b < 1048576)    return String.format("%.1f KB", b / 1024.0);
        if (b < 1073741824) return String.format("%.1f MB", b / 1048576.0);
        return String.format("%.2f GB", b / 1073741824.0);
    }
}
