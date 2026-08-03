package com.shaurma.packmanager.core;

import com.shaurma.packmanager.config.PackManagerConfig;
import javafx.stage.DirectoryChooser;
import javafx.stage.FileChooser;

import java.io.File;
import java.nio.file.Files;
import java.nio.file.Path;

public final class FileDialogMemory {

    private static Path lastDir;
    private static Path memoryFile;

    private FileDialogMemory() {}

    private static Path memoryFile() {
        if (memoryFile == null) {
            memoryFile = PackManagerConfig.workDir().resolve("last-chooser-dir.txt");
        }
        return memoryFile;
    }

    public static void rememberParentOf(File file) {
        if (file != null && file.getParentFile() != null) {
            lastDir = file.getParentFile().toPath();
            save();
        }
    }

    public static void applyTo(FileChooser fc) {
        load();
        if (lastDir != null && Files.exists(lastDir)) {
            fc.setInitialDirectory(lastDir.toFile());
        }
    }

    public static void applyTo(DirectoryChooser dc) {
        load();
        if (lastDir != null && Files.exists(lastDir)) {
            dc.setInitialDirectory(lastDir.toFile());
        }
    }

    private static void load() {
        if (lastDir != null) return;
        Path f = memoryFile();
        if (Files.exists(f)) {
            try {
                String s = Files.readString(f).trim();
                if (!s.isEmpty()) lastDir = Path.of(s);
            } catch (Exception ignored) {}
        }
    }

    private static void save() {
        try {
            Path f = memoryFile();
            Files.createDirectories(f.getParent());
            Files.writeString(f, lastDir.toString());
        } catch (Exception ignored) {}
    }
}
