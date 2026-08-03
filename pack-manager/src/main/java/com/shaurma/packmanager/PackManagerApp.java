package com.shaurma.packmanager;

import com.shaurma.packmanager.config.PackManagerConfig;
import com.shaurma.packmanager.ui.screens.MainScreen;
import javafx.application.Application;
import javafx.scene.Scene;
import javafx.scene.image.Image;
import javafx.stage.Stage;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.nio.file.Files;
import java.nio.file.Path;

/**
 * ╔══════════════════════════════════════════════════════════════════════════╗
 * ║  Shaurma Pack Manager                                                   ║
 * ║  Окрема програма для керування modpack-збірками та публікації на R2.   ║
 * ╚══════════════════════════════════════════════════════════════════════════╝
 *
 * Запускається окремо від лаунчера.
 * Генерує server-manifest.json і завантажує файли на Cloudflare R2.
 */
public class PackManagerApp extends Application {

    private static final Logger log = LoggerFactory.getLogger(PackManagerApp.class);

    public static void main(String[] args) {
        launch(args);
    }

    @Override
    public void start(Stage stage) throws Exception {
        // Ініціалізація конфігурації
        PackManagerConfig.init();
        log.info("Pack Manager запущено. Конфіг: {}", PackManagerConfig.settingsFile());

        // Головний екран
        MainScreen mainScreen = new MainScreen(stage);
        Scene scene = new Scene(mainScreen.getRoot(), 1200, 800);

        // CSS
        scene.getStylesheets().add(
            getClass().getResource("/css/pack-manager.css").toExternalForm());

        stage.setScene(scene);
        stage.setTitle("Shaurma Pack Manager");
        stage.setMinWidth(900);
        stage.setMinHeight(600);

        // Іконка якщо є
        try {
            stage.getIcons().add(new Image(
                getClass().getResourceAsStream("/icon.png")));
        } catch (Exception ignored) {}

        stage.show();
        log.info("Pack Manager GUI запущено");
    }

    @Override
    public void stop() {
        log.info("Pack Manager закрито");
    }
}
