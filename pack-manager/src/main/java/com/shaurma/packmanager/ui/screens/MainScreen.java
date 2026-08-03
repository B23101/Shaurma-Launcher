package com.shaurma.packmanager.ui.screens;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.shaurma.packmanager.config.PackManagerConfig;
import com.shaurma.packmanager.core.*;
import com.shaurma.packmanager.model.PackProject;
import java.io.File;
import javafx.application.Platform;
import javafx.concurrent.Task;
import javafx.collections.FXCollections;
import javafx.collections.ObservableList;
import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.scene.control.*;
import javafx.scene.layout.*;
import javafx.scene.text.Font;
import javafx.stage.DirectoryChooser;
import javafx.stage.FileChooser;
import javafx.stage.Stage;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.*;
import java.nio.file.*;
import java.security.MessageDigest;
import java.time.Instant;
import java.util.*;
import java.util.stream.Collectors;
import java.util.function.Consumer;

/**
 * Головний екран Pack Manager.
 * Складається з бічної панелі навігації і основної робочої зони.
 *
 * Екрани:
 *  - Список збірок (головний)
 *  - Редагування збірки (вкладки: Інформація, Файли, Правила, JSON)
 *  - Cloudflare R2
 *  - Deploy
 *  - Налаштування
 */
public class MainScreen {

    private static final Logger log = LoggerFactory.getLogger(MainScreen.class);
    private static final Gson   GSON = new GsonBuilder().setPrettyPrinting().create();

    private final Stage stage;
    private Runnable onMrpackSelected;
    private final BorderPane root;
    private final StackPane  contentArea;

    // Стан
    private final ObservableList<PackProject> packs = FXCollections.observableArrayList();
    private PackProject selectedPack;

    // Зберігаємо файл проекту
    private Path projectFile;

    public MainScreen(Stage stage) {
        this.stage = stage;
        this.root  = new BorderPane();
        this.contentArea = new StackPane();

        buildLayout();
        loadProjectFromDisk();
        showPackList();
    }

    public BorderPane getRoot() { return root; }

    // ══════════════════════════════════════════════════════════════════════
    //  Layout
    // ══════════════════════════════════════════════════════════════════════

    private void buildLayout() {
        root.setLeft(buildSidebar());
        root.setCenter(contentArea);
        root.getStyleClass().add("main-root");
    }

    private VBox buildSidebar() {
        VBox sidebar = new VBox(4);
        sidebar.getStyleClass().add("sidebar");
        sidebar.setPrefWidth(200);
        sidebar.setPadding(new Insets(16, 8, 16, 8));

        Label title = new Label("Pack Manager");
        title.getStyleClass().add("sidebar-title");

        sidebar.getChildren().addAll(
            title,
            new Separator(),
            sidebarBtn("Збірки",     this::showPackList),
            sidebarBtn("Cloudflare R2", this::showR2Screen),
            sidebarBtn("Deploy",       this::showDeployScreen),
            sidebarBtn("Оновлення лаунчера", this::showLauncherUpdateScreen),
            sidebarBtn("Налаштування", this::showSettingsScreen)
        );
        return sidebar;
    }

    private Button sidebarBtn(String text, Runnable action) {
        Button btn = new Button(text);
        btn.getStyleClass().add("sidebar-btn");
        btn.setMaxWidth(Double.MAX_VALUE);
        btn.setOnAction(e -> action.run());
        return btn;
    }

    private void setContent(javafx.scene.Node node) {
        contentArea.getChildren().setAll(node);
    }

    // ══════════════════════════════════════════════════════════════════════
    //  Список збірок
    // ══════════════════════════════════════════════════════════════════════

    private void showPackList() {
        VBox page = new VBox(16);
        page.getStyleClass().add("page");
        page.setPadding(new Insets(24));

        HBox header = new HBox(12);
        header.setAlignment(Pos.CENTER_LEFT);
        Label title = new Label("Модпаки");
        title.getStyleClass().add("page-title");
        Region spacer = new Region();
        HBox.setHgrow(spacer, Priority.ALWAYS);
        Button addBtn = new Button("+ Нова збірка");
        addBtn.getStyleClass().addAll("btn", "btn-primary");
        addBtn.setOnAction(e -> showNewPackDialog());
        header.getChildren().addAll(title, spacer, addBtn);

        FlowPane cards = new FlowPane(16, 16);
        cards.getStyleClass().add("cards-pane");
        refreshPackCards(cards);

        ScrollPane scroll = new ScrollPane(cards);
        scroll.setFitToWidth(true);
        scroll.getStyleClass().add("cards-scroll");
        VBox.setVgrow(scroll, Priority.ALWAYS);

        page.getChildren().addAll(header, scroll);
        setContent(page);
    }

    private void refreshPackCards(FlowPane cards) {
        cards.getChildren().clear();
        for (PackProject pack : packs) {
            cards.getChildren().add(buildPackCard(pack));
        }
    }

    private VBox buildPackCard(PackProject pack) {
        VBox card = new VBox(8);
        card.getStyleClass().addAll("pack-card");
        card.setPrefWidth(300);
        card.setPadding(new Insets(16));

        Label name = new Label(pack.getName() != null ? pack.getName() : "(без назви)");
        name.getStyleClass().add("pack-card-name");

        String loaderInfo = (pack.getLoader() != null ? pack.getLoader() : "?") +
            (pack.getLoaderVersion() != null ? " " + pack.getLoaderVersion() : "");
        Label meta = new Label(
            "MC " + (pack.getMcVersion() != null ? pack.getMcVersion() : "?") +
            " · " + loaderInfo);
        meta.getStyleClass().add("pack-card-meta");

        HBox tagsBox = new HBox(6);
        if (pack.getTags() != null) {
            pack.getTags().forEach(tag -> {
                Label tagLabel = new Label(tag);
                tagLabel.getStyleClass().add("tag");
                tagsBox.getChildren().add(tagLabel);
            });
        }

        Label statusLabel = new Label(pack.isDirty() ? "⬤ Не задеплоєно" : "✓ Актуальна");
        statusLabel.getStyleClass().add(pack.isDirty() ? "status-dirty" : "status-clean");

        if (pack.getLastDeployed() != null) {
            Label deployed = new Label("Deploy: " + pack.getLastDeployed().substring(0, 10));
            deployed.getStyleClass().add("pack-card-meta");
            card.getChildren().add(deployed);
        }

        HBox btns = new HBox(8);
        Button editBtn = new Button("✏ Редагувати");
        editBtn.getStyleClass().addAll("btn", "btn-default");
        editBtn.setOnAction(e -> showEditPack(pack));

        Button deleteBtn = new Button("🗑");
        deleteBtn.getStyleClass().addAll("btn", "btn-danger");
        Tooltip.install(deleteBtn, new Tooltip("Видалити збірку з сервера"));
        deleteBtn.setOnAction(e -> deletePack(pack));

        Button deployBtn = new Button("Deploy");
        deployBtn.getStyleClass().addAll("btn", "btn-primary");
        deployBtn.setOnAction(e -> quickDeploy(pack));

        btns.getChildren().addAll(editBtn, deleteBtn, deployBtn);

        card.getChildren().addAll(name, meta, tagsBox, statusLabel, btns);
        return card;
    }

    // ══════════════════════════════════════════════════════════════════════
    //  Редагування збірки
    // ══════════════════════════════════════════════════════════════════════

    // ══════════════════════════════════════════════════════════════════════
    //  Delete Pack
    // ══════════════════════════════════════════════════════════════════════

    private void deletePack(PackProject pack) {
        Alert confirm = new Alert(Alert.AlertType.CONFIRMATION);
        confirm.setTitle("Видалити збірку");
        confirm.setHeaderText("Видалити «" + pack.getName() + "» з сервера?");
        confirm.setContentText(
            "Будуть видалені всі файли з R2:\n" +
            "  • packs/" + pack.getId() + "/\n" +
            "  • server-manifest.json (оновлено)\n\n" +
            "Локальні файли НЕ видаляються.");

        confirm.showAndWait().ifPresent(btn -> {
            if (btn != ButtonType.OK) return;

            PackManagerConfig.R2Settings r2cfg = PackManagerConfig.settings().r2();
            if (!r2cfg.isConfigured()) {
                alert("R2 не налаштовано");
                return;
            }

            Task<Void> task = new Task<>() {
                @Override protected Void call() throws Exception {
                    try (R2Client r2 = new R2Client(r2cfg)) {
                        // Видаляємо всі файли збірки з R2
                        updateMessage("Видалення файлів з R2...");
                        String prefix = "packs/" + pack.getId() + "/";
                        for (R2Client.ListEntry obj : r2.list(prefix)) {
                            r2.delete(obj.key());
                            log.info("Deleted: {}", obj.key());
                        }

                        // Видаляємо збірку з server-manifest.json напряму (без ManifestGenerator — він блокус UI та зберігає старі записи)
                        updateMessage("Оновлення server-manifest.json...");
                        String existingJson = r2.getString("server-manifest.json");
                        if (existingJson != null && !existingJson.isBlank()) {
                            JsonObject root = JsonParser.parseString(existingJson).getAsJsonObject();
                            JsonArray packsArray = root.getAsJsonArray("packs");
                            JsonArray newPacks = new JsonArray();
                            String packId = pack.getId();
                            for (JsonElement el : packsArray) {
                                JsonObject obj = el.getAsJsonObject();
                                String id = obj.has("id") ? obj.get("id").getAsString() : null;
                                if (id != null && !id.equals(packId)) {
                                    newPacks.add(obj);
                                }
                            }
                            root.add("packs", newPacks);
                            root.addProperty("generatedAt", java.time.Instant.now().toString());
                            R2Client.UploadResult result = r2.putString("server-manifest.json",
                                GSON.toJson(root), "application/json");
                            if (!result.success()) {
                                throw new RuntimeException("Не вдалось оновити server-manifest.json: " + result.error());
                            }
                            updateMessage("✓ server-manifest.json оновлено");
                        }
                    }
                    // Видаляємо з локального списку на JavaFX потоці
                    Platform.runLater(() -> {
                        packs.remove(pack);
                        showPackList();
                        alert("Збірку «" + pack.getName() + "» видалено з сервера.");
                    });
                    return null;
                }
            };
            task.messageProperty().addListener((obs, o, n) -> log.info("Delete: {}", n));
            task.setOnFailed(e -> Platform.runLater(() ->
                alert("Помилка видалення: " + task.getException().getMessage())));
            new Thread(task, "pack-delete").start();
        });
    }

    private void showEditPack(PackProject pack) {
        selectedPack = pack;

        VBox page = new VBox(0);
        page.getStyleClass().add("page");

        HBox header = new HBox(12);
        header.getStyleClass().add("page-header");
        header.setAlignment(Pos.CENTER_LEFT);
        header.setPadding(new Insets(16, 24, 16, 24));

        Button backBtn = new Button("← Назад");
        backBtn.getStyleClass().addAll("btn", "btn-ghost");
        backBtn.setOnAction(e -> showPackList());

        Label title = new Label("Редагування: " + pack.getName());
        title.getStyleClass().add("page-title");

        Region spacer = new Region();
        HBox.setHgrow(spacer, Priority.ALWAYS);

        Button saveBtn = new Button("💾 Зберегти");
        saveBtn.getStyleClass().addAll("btn", "btn-primary");
        saveBtn.setOnAction(e -> saveProject());

        header.getChildren().addAll(backBtn, title, spacer, saveBtn);

        TabPane tabs = new TabPane();
        tabs.getStyleClass().add("edit-tabs");
        tabs.setTabClosingPolicy(TabPane.TabClosingPolicy.UNAVAILABLE);
        VBox.setVgrow(tabs, Priority.ALWAYS);

        tabs.getTabs().addAll(
            buildInfoTab(pack),
            buildFilesTab(pack),
            buildSyncRulesTab(pack),
            buildManifestTab(pack)
        );

        page.getChildren().addAll(header, tabs);
        setContent(page);
    }

    private Tab buildInfoTab(PackProject pack) {
        Tab tab = new Tab("📋 Інформація");
        VBox form = new VBox(16);
        form.getStyleClass().add("form");
        form.setPadding(new Insets(24));

        TextField mcVerField     = packField(pack.getMcVersion(), v -> pack.setMcVersion(v));
        TextField loaderVerField = packField(pack.getLoaderVersion(), v -> pack.setLoaderVersion(v));
        Label loaderLabel = new Label(pack.getLoader() != null ? pack.getLoader() : "—");
        loaderLabel.getStyleClass().add("form-readonly-value");

        mcVerField.setEditable(false);
        mcVerField.setPromptText("Визначається автоматично з .mrpack");
        loaderVerField.setEditable(false);
        loaderVerField.setPromptText("Визначається автоматично з .mrpack");

        Runnable refreshFromMrpack = () -> {
            if (pack.getMrpackPath() == null || pack.getMrpackPath().isBlank()) return;
            MrpackAnalyzer.AnalysisResult res =
                MrpackAnalyzer.analyze(java.nio.file.Path.of(pack.getMrpackPath()));
            if (res.success()) {
                pack.setMcVersion(res.mcVersion());
                mcVerField.setText(res.mcVersion());
                if (res.loader() != null) {
                    pack.setLoader(res.loader());
                    loaderLabel.setText(res.loader());
                }
                if (res.loaderVersion() != null) {
                    pack.setLoaderVersion(res.loaderVersion());
                    loaderVerField.setText(res.loaderVersion());
                }
            } else {
                alert("Не вдалось проаналізувати .mrpack: " + res.error());
            }
        };
        // Зберігаємо для виклику після вибору файлу в buildMrpackUrlRow
        this.onMrpackSelected = refreshFromMrpack;

        Button reanalyzeBtn = new Button("🔍 Переаналізувати");
        reanalyzeBtn.getStyleClass().addAll("btn", "btn-default");
        reanalyzeBtn.setOnAction(e -> refreshFromMrpack.run());

        form.getChildren().addAll(
            formRow("ID збірки:", packField(pack.getId(), v -> pack.setId(v))),
            formRow("Назва:", packField(pack.getName(), v -> pack.setName(v))),
            formRow("Версія MC:", mcVerField),
            formRow("Лоадер:", loaderLabel),
            formRow("Версія лоадера:", loaderVerField),
            formRow("", reanalyzeBtn),
            formRow("URL mrpack:", buildMrpackUrlRow(pack)),
            formRow("Іконка (icon.png):", buildFilePickerRow(pack.getIconPath(),
                v -> pack.setIconPath(v), "Виберіть іконку",
                new javafx.stage.FileChooser.ExtensionFilter("Зображення", "*.png", "*.jpg", "*.jpeg"))),
            formRow("Фон (background):", buildFilePickerRow(pack.getBackgroundPath(),
                v -> pack.setBackgroundPath(v), "Виберіть фон",
                new javafx.stage.FileChooser.ExtensionFilter("Зображення", "*.png", "*.jpg", "*.jpeg", "*.webp"))),
            formRow("Опис:", packTextArea(pack.getDescription(), v -> pack.setDescription(v))),
            formRow("Теги:", buildTagsEditor(pack)),
            formRow("IP сервера:", buildServerIpRow(pack))
        );

        ScrollPane scroll = new ScrollPane(form);
        scroll.setFitToWidth(true);
        tab.setContent(scroll);
        return tab;
    }

    private HBox buildMrpackUrlRow(PackProject pack) {
        TextField urlField = new TextField(pack.getMrpackUrl() != null ? pack.getMrpackUrl() : "");
        urlField.setPromptText("https://cdn.r2.example.com/pack.mrpack або залиш порожнім");
        urlField.textProperty().addListener((obs, o, n) -> pack.setMrpackUrl(n));
        HBox.setHgrow(urlField, Priority.ALWAYS);

        Button browseBtn = new Button("📁 Файл");
        browseBtn.getStyleClass().addAll("btn", "btn-default");
        browseBtn.setOnAction(e -> {
            FileChooser fc = new FileChooser();
            fc.setTitle("Виберіть .mrpack файл");
            fc.getExtensionFilters().add(
                new FileChooser.ExtensionFilter("Modrinth Pack", "*.mrpack"));
            FileDialogMemory.applyTo(fc);
            File f = fc.showOpenDialog(stage);
            FileDialogMemory.rememberParentOf(f);
            if (f != null) {
                pack.setMrpackPath(f.getAbsolutePath());
                urlField.setText(f.getName() + " (локальний файл)");
                saveLocalPaths(); // одразу зберігаємо після вибору файлу
                if (onMrpackSelected != null) onMrpackSelected.run();
            }
        });

        HBox row = new HBox(8, urlField, browseBtn);
        row.setAlignment(Pos.CENTER_LEFT);
        return row;
    }

    private HBox buildServerIpRow(PackProject pack) {
        TextField ipField = new TextField(pack.getServerIp() != null ? pack.getServerIp() : "");
        ipField.setPromptText("server.ip:25565 або залиш порожнім");
        ipField.textProperty().addListener((obs, o, n) -> pack.setServerIp(n.isBlank() ? null : n));
        HBox.setHgrow(ipField, Priority.ALWAYS);

        Label hint = new Label("Авто-підключення до сервера");
        hint.setStyle("-fx-font-size: 11px; -fx-text-fill: rgba(255,255,255,0.3);");

        HBox row = new HBox(8, ipField, hint);
        row.setAlignment(Pos.CENTER_LEFT);
        return row;
    }

    private ComboBox<String> buildLoaderSelect(PackProject pack) {
        ComboBox<String> combo = new ComboBox<>(FXCollections.observableArrayList(
            "FABRIC", "FORGE", "NEOFORGE", "QUILT", "VANILLA"));
        combo.setValue(pack.getLoader() != null ? pack.getLoader() : "FABRIC");
        combo.setOnAction(e -> pack.setLoader(combo.getValue()));
        combo.getStyleClass().add("form-combo");
        return combo;
    }

    private HBox buildTagsEditor(PackProject pack) {
        FlowPane tagsPane = new FlowPane(6, 6);
        TextField tagInput = new TextField();
        tagInput.setPromptText("Додати тег...");
        tagInput.setPrefWidth(150);
        Button addTagBtn = new Button("+");
        addTagBtn.getStyleClass().addAll("btn", "btn-default");

        Runnable[] refreshTagsHolder = new Runnable[1];
        refreshTagsHolder[0] = () -> {
            tagsPane.getChildren().clear();
            if (pack.getTags() != null) {
                pack.getTags().forEach(tag -> {
                    HBox tagBox = new HBox(4);
                    tagBox.getStyleClass().add("tag-editor");
                    Label l = new Label(tag);
                    Button del = new Button("×");
                    del.getStyleClass().add("tag-del");
                    del.setOnAction(ev -> {
                        pack.getTags().remove(tag);
                        refreshTagsHolder[0].run();
                    });
                    tagBox.getChildren().addAll(l, del);
                    tagsPane.getChildren().add(tagBox);
                });
            }
        };
        refreshTagsHolder[0].run();

        addTagBtn.setOnAction(e -> {
            String tag = tagInput.getText().trim();
            if (!tag.isEmpty()) {
                if (pack.getTags() == null) pack.setTags(new ArrayList<>());
                if (!pack.getTags().contains(tag)) {
                    pack.getTags().add(tag);
                    refreshTagsHolder[0].run();
                }
                tagInput.clear();
            }
        });

        VBox result = new VBox(8, tagsPane, new HBox(6, tagInput, addTagBtn));
        return new HBox(result);
    }

    private Tab buildFilesTab(PackProject pack) {
        Tab tab = new Tab("📁 Файли та папки");

        VBox content = new VBox(12);
        content.setPadding(new Insets(16));

        TableView<Map.Entry<String, String>> table = new TableView<>();
        table.getStyleClass().add("sync-table");
        VBox.setVgrow(table, Priority.ALWAYS);

        TableColumn<Map.Entry<String, String>, String> pathCol = new TableColumn<>("Шлях");
        pathCol.setCellValueFactory(c -> new javafx.beans.property.SimpleStringProperty(c.getValue().getKey()));
        pathCol.setPrefWidth(280);

        TableColumn<Map.Entry<String, String>, String> ruleCol = new TableColumn<>("Правило");
        ruleCol.setCellValueFactory(c -> new javafx.beans.property.SimpleStringProperty(c.getValue().getValue()));
        ruleCol.setCellFactory(col -> new TableCell<>() {
            @Override
            protected void updateItem(String item, boolean empty) {
                super.updateItem(item, empty);
                if (empty || item == null) { setText(null); setGraphic(null); return; }
                Label badge = new Label(item);
                badge.getStyleClass().addAll("badge", PackProject.ruleStyleClass(item));
                setGraphic(badge);
                setText(null);
            }
        });
        ruleCol.setPrefWidth(160);

        TableColumn<Map.Entry<String, String>, String> actionsCol = new TableColumn<>("Дії");
        actionsCol.setCellFactory(col -> new TableCell<>() {
            @Override
            protected void updateItem(String item, boolean empty) {
                super.updateItem(item, empty);
                if (empty) { setGraphic(null); return; }
                Map.Entry<String, String> entry = getTableRow().getItem();
                if (entry == null) { setGraphic(null); return; }

                Button editBtn = new Button("✏");
                editBtn.getStyleClass().add("icon-btn");
                editBtn.setOnAction(e -> showEditRuleDialog(pack, entry.getKey(), table));

                Button delBtn = new Button("🗑");
                delBtn.getStyleClass().addAll("icon-btn", "btn-danger");
                delBtn.setOnAction(e -> {
                    pack.removeSyncRule(entry.getKey());
                    table.setItems(FXCollections.observableArrayList(pack.getSyncRules().entrySet()));
                });

                HBox btns = new HBox(4, editBtn, delBtn);
                setGraphic(btns);
            }
        });
        actionsCol.setPrefWidth(80);

        table.getColumns().addAll(pathCol, ruleCol, actionsCol);
        table.setItems(FXCollections.observableArrayList(pack.getSyncRules().entrySet()));

        HBox toolbar = new HBox(8);
        Button addRuleBtn = new Button("+ Додати шлях/правило");
        addRuleBtn.getStyleClass().addAll("btn", "btn-default");
        addRuleBtn.setOnAction(e -> showAddRuleDialog(pack, table));

        Button resetBtn = new Button("↺ Відновити дефолтні");
        resetBtn.getStyleClass().addAll("btn", "btn-ghost");
        resetBtn.setOnAction(e -> {
            pack.getSyncRules().clear();
            pack.getSyncRules().putAll(PackProject.defaultSyncRules());
            table.setItems(FXCollections.observableArrayList(pack.getSyncRules().entrySet()));
        });

        toolbar.getChildren().addAll(addRuleBtn, resetBtn);
        content.getChildren().addAll(toolbar, table);
        tab.setContent(content);
        return tab;
    }

    private Tab buildSyncRulesTab(PackProject pack) {
        Tab tab = new Tab("🔧 Правила");
        VBox content = new VBox(16);
        content.setPadding(new Insets(24));

        Label description = new Label("Правила визначають як лаунчер оновлює кожну папку:");
        description.getStyleClass().add("section-desc");

        for (String rule : PackProject.ALL_RULES) {
            VBox ruleBox = new VBox(4);
            ruleBox.getStyleClass().addAll("rule-box", PackProject.ruleStyleClass(rule));
            ruleBox.setPadding(new Insets(12));

            HBox ruleHeader = new HBox(8);
            ruleHeader.setAlignment(Pos.CENTER_LEFT);
            Label ruleBadge = new Label(rule);
            ruleBadge.getStyleClass().addAll("badge", PackProject.ruleStyleClass(rule));

            List<String> pathsUsingRule = new ArrayList<>();
            pack.getSyncRules().forEach((path, r) -> { if (rule.equals(r)) pathsUsingRule.add(path); });
            Label paths = new Label(pathsUsingRule.isEmpty()
                ? "(не використовується)"
                : String.join(", ", pathsUsingRule));
            paths.getStyleClass().add("rule-paths");

            ruleHeader.getChildren().addAll(ruleBadge, paths);

            Label desc = new Label(PackProject.ruleDescription(rule));
            desc.getStyleClass().add("rule-desc");
            desc.setWrapText(true);

            ruleBox.getChildren().addAll(ruleHeader, desc);
            content.getChildren().add(ruleBox);
        }

        ScrollPane scroll = new ScrollPane(content);
        scroll.setFitToWidth(true);
        tab.setContent(scroll);
        return tab;
    }

    private Tab buildManifestTab(PackProject pack) {
        Tab tab = new Tab("{ } Manifest JSON");

        VBox content = new VBox(12);
        content.setPadding(new Insets(16));

        TextArea jsonArea = new TextArea();
        jsonArea.getStyleClass().add("json-area");
        jsonArea.setEditable(false);
        jsonArea.setFont(Font.font("Monospace", 12));
        VBox.setVgrow(jsonArea, Priority.ALWAYS);

        HBox toolbar = new HBox(8);
        Button genBtn = new Button("⚙ Генерувати");
        genBtn.getStyleClass().addAll("btn", "btn-primary");
        genBtn.setOnAction(e -> {
            try (R2Client r2 = R2Client.fromConfig()) {
                ManifestGenerator gen = new ManifestGenerator(r2);
                ManifestGenerator.GenerationResult result = gen.generate(List.of(pack));
                if (result.success()) {
                    jsonArea.setText(result.json());
                } else {
                    jsonArea.setText("Помилки:\n" + String.join("\n", result.warnings()));
                }
            } catch (Exception ex) {
                jsonArea.setText("Помилка: " + ex.getMessage());
            }
        });

        Button copyBtn = new Button("📋 Копіювати");
        copyBtn.getStyleClass().addAll("btn", "btn-default");
        copyBtn.setOnAction(e -> {
            javafx.scene.input.ClipboardContent cc = new javafx.scene.input.ClipboardContent();
            cc.putString(jsonArea.getText());
            javafx.scene.input.Clipboard.getSystemClipboard().setContent(cc);
        });

        Button deployManifestBtn = new Button("🚀 Завантажити на R2");
        deployManifestBtn.getStyleClass().addAll("btn", "btn-primary");
        // Deploy цього пака через V2 (Deploy-екран зосередиться на ньому).
        // Перевірка «спочатку згенеруйте маніфест» прибрана: публікація
        // більше не залежить від V1-прев'ю в цій вкладці.
        deployManifestBtn.setOnAction(e -> quickDeploy(pack));

        toolbar.getChildren().addAll(genBtn, copyBtn, deployManifestBtn);
        content.getChildren().addAll(toolbar, jsonArea);
        tab.setContent(content);
        return tab;
    }

    // ══════════════════════════════════════════════════════════════════════
    //  R2 Screen
    // ══════════════════════════════════════════════════════════════════════

    private void showR2Screen() {
        VBox page = new VBox(20);
        page.getStyleClass().add("page");
        page.setPadding(new Insets(24));

        Label title = new Label("Cloudflare R2");
        title.getStyleClass().add("page-title");

        PackManagerConfig.R2Settings cfg = PackManagerConfig.settings().r2();

        VBox connectForm = new VBox(12);
        connectForm.getStyleClass().add("card");
        connectForm.setPadding(new Insets(16));
        Label connectTitle = new Label("Налаштування підключення");
        connectTitle.getStyleClass().add("card-title");

        TextField accountIdField = formField("Account ID", cfg.accountId());
        TextField accessKeyField = formField("Access Key ID", cfg.accessKeyId());
        PasswordField secretField = new PasswordField();
        secretField.setText(cfg.secretAccessKey());
        secretField.setPromptText("Secret Access Key");
        secretField.getStyleClass().add("form-field");
        TextField bucketField   = formField("Bucket Name", cfg.bucketName());
        TextField domainField   = formField("Public Domain (CDN URL)", cfg.publicDomain());
        TextField endpointField = formField("Endpoint (залиш порожнім для auto)", cfg.endpoint());

        Label statusLabel = new Label();
        statusLabel.getStyleClass().add("status-label");

        Button testBtn = new Button("🔌 Перевірити з'єднання");
        testBtn.getStyleClass().addAll("btn", "btn-default");
        testBtn.setOnAction(e -> {
            PackManagerConfig.R2Settings newCfg = new PackManagerConfig.R2Settings(
                accountIdField.getText(), accessKeyField.getText(), secretField.getText(),
                bucketField.getText(), domainField.getText(), endpointField.getText());
            try (R2Client r2 = new R2Client(newCfg)) {
                String err = r2.testConnection();
                if (err == null) {
                    statusLabel.setText("✓ З'єднання успішне");
                    statusLabel.getStyleClass().removeAll("status-error");
                    statusLabel.getStyleClass().add("status-ok");
                } else {
                    statusLabel.setText("✗ Помилка: " + err);
                    statusLabel.getStyleClass().removeAll("status-ok");
                    statusLabel.getStyleClass().add("status-error");
                }
            } catch (Exception ex) {
                statusLabel.setText("✗ " + ex.getMessage());
                statusLabel.getStyleClass().removeAll("status-ok");
                statusLabel.getStyleClass().add("status-error");
            }
        });

        Button saveR2Btn = new Button("💾 Зберегти");
        saveR2Btn.getStyleClass().addAll("btn", "btn-primary");
        saveR2Btn.setOnAction(e -> {
            PackManagerConfig.R2Settings newCfg = new PackManagerConfig.R2Settings(
                accountIdField.getText(), accessKeyField.getText(), secretField.getText(),
                bucketField.getText(), domainField.getText(), endpointField.getText());
            try {
                PackManagerConfig.updateSettings(PackManagerConfig.settings().withR2(newCfg));
                statusLabel.setText("✓ Збережено");
                statusLabel.getStyleClass().removeAll("status-error");
                statusLabel.getStyleClass().add("status-ok");
            } catch (IOException ex) {
                statusLabel.setText("✗ " + ex.getMessage());
            }
        });

        connectForm.getChildren().addAll(
            connectTitle,
            formRow("Account ID:", accountIdField),
            formRow("Access Key ID:", accessKeyField),
            formRow("Secret Access Key:", secretField),
            formRow("Bucket Name:", bucketField),
            formRow("Public Domain:", domainField),
            formRow("Endpoint:", endpointField),
            new HBox(8, testBtn, saveR2Btn),
            statusLabel
        );

        VBox filesCard = new VBox(12);
        filesCard.getStyleClass().add("card");
        filesCard.setPadding(new Insets(16));
        Label filesTitle = new Label("Файли на R2");
        filesTitle.getStyleClass().add("card-title");

        TableView<R2Client.ListEntry> filesTable = new TableView<>();
        filesTable.setPrefHeight(300);

        TableColumn<R2Client.ListEntry, String> keyCol = new TableColumn<>("Ключ");
        keyCol.setCellValueFactory(c -> new javafx.beans.property.SimpleStringProperty(c.getValue().key()));
        keyCol.setPrefWidth(400);

        TableColumn<R2Client.ListEntry, String> sizeCol = new TableColumn<>("Розмір");
        sizeCol.setCellValueFactory(c -> new javafx.beans.property.SimpleStringProperty(
            formatBytes(c.getValue().size())));
        sizeCol.setPrefWidth(120);

        TableColumn<R2Client.ListEntry, String> dateCol = new TableColumn<>("Дата");
        dateCol.setCellValueFactory(c -> new javafx.beans.property.SimpleStringProperty(
            c.getValue().lastModified()));
        dateCol.setPrefWidth(200);

        filesTable.getColumns().addAll(keyCol, sizeCol, dateCol);

        Button refreshFilesBtn = new Button("🔄 Оновити список");
        refreshFilesBtn.getStyleClass().addAll("btn", "btn-default");
        refreshFilesBtn.setOnAction(e -> {
            try (R2Client r2 = R2Client.fromConfig()) {
                List<R2Client.ListEntry> entries = r2.list(null);
                filesTable.setItems(FXCollections.observableArrayList(entries));
            } catch (Exception ex) {
                alert("Помилка завантаження списку: " + ex.getMessage());
            }
        });

        filesCard.getChildren().addAll(filesTitle, refreshFilesBtn, filesTable);

        page.getChildren().addAll(title, connectForm, filesCard);
        ScrollPane scroll = new ScrollPane(page);
        scroll.setFitToWidth(true);
        setContent(scroll);
    }

    // ══════════════════════════════════════════════════════════════════════
    //  Deploy Screen
    // ══════════════════════════════════════════════════════════════════════

    private void showDeployScreen() {
        showDeployScreen(null);
    }

    // showDeployScreen(only) — Deploy-екран, зосереджений на ОДНОМУ паку
    // (quickDeploy): у списку лише він, і він уже вибраний. only == null —
    // загальний екран з усіма збірками (dirty обрані за замовчуванням).
    private void showDeployScreen(PackProject only) {
        VBox page = new VBox(16);
        page.getStyleClass().add("page");
        page.setPadding(new Insets(24));

        Label title = new Label(only != null ? "Deploy: " + only.getName() : "Deploy на R2");
        title.getStyleClass().add("page-title");

        VBox packsCard = new VBox(8);
        packsCard.getStyleClass().add("card");
        packsCard.setPadding(new Insets(16));
        Label selectTitle = new Label(only != null ? "Збірка для деплою:" : "Виберіть збірки:");
        selectTitle.getStyleClass().add("card-title");

        Map<PackProject, CheckBox> checkBoxMap = new LinkedHashMap<>();
        List<PackProject> scope = (only != null) ? List.of(only) : packs;
        for (PackProject pack : scope) {
            CheckBox cb = new CheckBox(pack.getName() +
                (pack.isDirty() ? " ⬤" : " ✓"));
            cb.setSelected(only != null || pack.isDirty());
            checkBoxMap.put(pack, cb);
        }
        packsCard.getChildren().add(selectTitle);
        packsCard.getChildren().addAll(checkBoxMap.values());

        TextArea logArea = new TextArea();
        logArea.setEditable(false);
        logArea.getStyleClass().add("log-area");
        logArea.setFont(Font.font("Monospace", 11));
        logArea.setPrefHeight(300);
        VBox.setVgrow(logArea, Priority.ALWAYS);

        ProgressBar progressBar = new ProgressBar(0);
        progressBar.setMaxWidth(Double.MAX_VALUE);
        progressBar.getStyleClass().add("deploy-progress");

        Label progressLabel = new Label("Готово до deploy");
        progressLabel.getStyleClass().add("progress-label");

        Button deployBtn = new Button("Почати Deploy");
        deployBtn.getStyleClass().addAll("btn", "btn-primary", "btn-large");
        deployBtn.setOnAction(e -> {
            List<PackProject> selected = checkBoxMap.entrySet().stream()
                .filter(en -> en.getValue().isSelected())
                .map(Map.Entry::getKey)
                .toList();

            if (selected.isEmpty()) { alert("Виберіть хоча б одну збірку"); return; }

            logArea.clear();
            progressBar.setProgress(-1);
            deployBtn.setDisable(true);

            // DeployServiceV2 (DOWNLOAD_SYNC_DESIGN_V2.md): .mrpack розпаковується
            // локально при деплої, файли заливаються окремо, manifest.json —
            // per-pack. .mrpack більше НЕ публікується на R2 як цілий файл.
            DeployServiceV2 service = new DeployServiceV2();
            service.deploy(selected, event -> Platform.runLater(() -> {
                switch (event) {
                    case DeployServiceV2.DeployEvent.Step s ->
                        progressLabel.setText("[" + s.stepNum() + "/" + s.totalSteps() + "] " + s.name());
                    case DeployServiceV2.DeployEvent.Log l ->
                        logArea.appendText("[" + l.level().name() + "] " + l.message() + "\n");
                    case DeployServiceV2.DeployEvent.Done d -> {
                        progressBar.setProgress(1.0);
                        deployBtn.setDisable(false);
                        progressLabel.setText(String.format(
                            "✓ Deploy завершено! Збірок: %d, залито: %d, без змін: %d, помилок: %d",
                            d.report().packsDeployed(), d.report().filesUploaded(),
                            d.report().filesSkipped(), d.report().filesErrored()));
                        saveLocalPaths();
                        saveLocalPacksMetadata(); // зберігаємо метадані після deploy
                    }
                    case DeployServiceV2.DeployEvent.Error err -> {
                        progressBar.setProgress(0);
                        deployBtn.setDisable(false);
                        progressLabel.setText("✗ Помилка: " + err.message());
                        logArea.appendText("[ERROR] " + err.message() + "\n");
                    }
                }
            }));
        });

        page.getChildren().addAll(
            title,
            packsCard,
            new Label("Прогрес:"),
            progressBar,
            progressLabel,
            new Label("Лог:"),
            logArea,
            deployBtn
        );

        ScrollPane scroll = new ScrollPane(page);
        scroll.setFitToWidth(true);
        setContent(scroll);
    }

    // ══════════════════════════════════════════════════════════════════════
    //  Launcher Upload Screen
    // ══════════════════════════════════════════════════════════════════════

    private void showLauncherUpdateScreen() {
        VBox page = new VBox(20);
        page.getStyleClass().add("launcher-update-root");
        page.setPadding(new Insets(24));

        // ── Brand header ──
        HBox brandRow = new HBox(14);
        brandRow.setAlignment(Pos.CENTER_LEFT);

        Label iconLabel = new Label("🐾");
        iconLabel.getStyleClass().add("launcher-update-icon-container");
        iconLabel.setStyle("-fx-font-size: 40px;");

        VBox brandText = new VBox(0);
        Label brandTitle = new Label("SHAURMA");
        brandTitle.getStyleClass().add("launcher-update-brand");
        Label brandSub = new Label("LAUNCHER");
        brandSub.getStyleClass().add("launcher-update-brand-sub");
        brandText.getChildren().addAll(brandTitle, brandSub);

        Label screenTitle = new Label("Оновлення лаунчера");
        screenTitle.setStyle("-fx-text-fill: #ffffff; -fx-font-size: 13px; -fx-font-weight: 600; -fx-padding: 0 0 2 0; -fx-font-family: 'Inter', 'Segoe UI', sans-serif;");

        brandRow.getChildren().addAll(iconLabel, brandText);

        // ── Main card ──
        VBox card = new VBox(14);
        card.getStyleClass().add("launcher-update-card");
        card.setMaxWidth(600);

        // ── Тип лаунчера ──
        Label typeLabel = new Label("Тип лаунчера:");
        typeLabel.getStyleClass().add("launcher-update-label");

        ToggleGroup typeGroup = new ToggleGroup();
        RadioButton rbShaurma = new RadioButton("Шаурма (зі збірками)");
        RadioButton rbBase    = new RadioButton("Базовий (без збірок)");
        rbShaurma.setToggleGroup(typeGroup);
        rbBase.setToggleGroup(typeGroup);
        rbShaurma.setSelected(true);
        rbShaurma.setStyle("-fx-text-fill: #ffffff;");
        rbBase.setStyle("-fx-text-fill: #ffffff;");

        HBox typeRow = new HBox(20, rbShaurma, rbBase);
        typeRow.setAlignment(Pos.CENTER_LEFT);

        // ── Тека app-image (dist\Shaurm Launcher або dist\Shaurm Launcher Base) ──
        // Замість вибору одного EXE тепер вибираємо ЦІЛУ ПАПКУ, зібрану
        // build.ps1 (новий Go-монорепо) -- вона вже містить launcher-version.json (новий
        // compare-by-hash формат з масивом components) поруч з усіма
        // реальними файлами (launcher-core.exe, jar'и, updater\shrm-updater.exe
        // і т.д.). Pack Manager лише читає цей маніфест і заливає кожен
        // компонент за його "path" у відповідне місце на R2 -- жодних
        // хешів тут більше не рахується вручну, вони вже правильні, бо
        // порахувані ПІСЛЯ jpackage прямо з готового app-image.
        TextField dirPathField = new TextField();
        dirPathField.setPromptText("Виберіть теку app-image (dist\\Shaurm Launcher)");
        dirPathField.setEditable(false);
        dirPathField.getStyleClass().add("launcher-update-field");
        HBox.setHgrow(dirPathField, Priority.ALWAYS);

        TextField versionField = new TextField();
        versionField.setPromptText("Визначиться з launcher-version.json");
        versionField.setEditable(false);
        versionField.getStyleClass().add("launcher-update-field");
        HBox.setHgrow(versionField, Priority.ALWAYS);

        TextArea releaseNotesArea = new TextArea();
        releaseNotesArea.setPromptText("Заповнюється з launcher-version.json (releaseNotes), можна відредагувати перед публікацією...");
        releaseNotesArea.setPrefRowCount(4);
        releaseNotesArea.getStyleClass().add("launcher-update-area");

        Label componentsInfoLbl = new Label("Компонентів: —");
        componentsInfoLbl.getStyleClass().add("launcher-update-label");
        componentsInfoLbl.setStyle("-fx-text-fill: rgba(255,255,255,0.55);");

        // Тримаємо розпарсений локальний маніфест тут, щоб uploadBtn міг
        // ним скористатись без повторного читання диска.
        final JsonObject[] loadedManifest = new JsonObject[1];
        final Path[] loadedDir = new Path[1];

        Button browseBtn = new Button("📁 Вибрати теку app-image");
        browseBtn.getStyleClass().add("launcher-update-browse-btn");
        browseBtn.setOnAction(ev -> {
            DirectoryChooser dc = new DirectoryChooser();
            dc.setTitle("Виберіть теку app-image (містить launcher-version.json)");
            FileDialogMemory.applyTo(dc);
            File dir = dc.showDialog(stage);
            FileDialogMemory.rememberParentOf(dir);
            if (dir == null) return;

            Path manifestPath = dir.toPath().resolve("launcher-version.json");
            if (!Files.exists(manifestPath)) {
                alert("У вибраній теці немає launcher-version.json.\n"
                    + "Спочатку зберіть лаунчер через build.ps1 (Go-монорепо) -- він генерує\n"
                    + "цей файл автоматично в корені app-image (dist\\Shaurm Launcher\\).");
                return;
            }

            try {
                String json = Files.readString(manifestPath);
                JsonObject manifest = GSON.fromJson(json, JsonObject.class);
                loadedManifest[0] = manifest;
                loadedDir[0] = dir.toPath();

                dirPathField.setText(dir.getAbsolutePath());
                String ver = manifest.has("version") ? manifest.get("version").getAsString() : "?";
                versionField.setText(ver);

                String notes = manifest.has("releaseNotes") ? manifest.get("releaseNotes").getAsString() : "";
                releaseNotesArea.setText(notes);

                int compCount = manifest.has("components") ? manifest.getAsJsonArray("components").size() : 0;
                componentsInfoLbl.setText("Компонентів: " + compCount + "  (версія " + ver + ")");
            } catch (Exception ex) {
                alert("Не вдалось прочитати launcher-version.json: " + ex.getMessage());
            }
        });

        HBox fileRow = new HBox(8, dirPathField, browseBtn);
        fileRow.setAlignment(Pos.CENTER_LEFT);

        Label fileLabel = new Label("Тека app-image");
        fileLabel.getStyleClass().add("launcher-update-label");
        Label verLabel = new Label("Версія");
        verLabel.getStyleClass().add("launcher-update-label");
        Label notesLabel = new Label("Release Notes");
        notesLabel.getStyleClass().add("launcher-update-label");

        VBox fieldsBox = new VBox(6);
        fieldsBox.getChildren().addAll(
            typeLabel, typeRow,
            new Separator(),
            fileLabel, fileRow,
            componentsInfoLbl,
            new Separator(),
            verLabel, versionField,
            new Separator(),
            notesLabel, releaseNotesArea
        );
        fieldsBox.setStyle("-fx-spacing: 8;");

        card.getChildren().add(fieldsBox);

        // ── Upload row + progress ──
        VBox uploadSection = new VBox(10);
        uploadSection.setStyle("-fx-background-color: rgba(0,0,0,0.2); -fx-background-radius: 12; -fx-padding: 16; -fx-border-color: rgba(255,255,255,0.04); -fx-border-radius: 12;");

        Label progressLabel = new Label("Готово");
        progressLabel.getStyleClass().add("launcher-update-progress-text");

        ProgressBar progressBar = new ProgressBar(0);
        progressBar.setMaxWidth(Double.MAX_VALUE);
        progressBar.getStyleClass().add("launcher-update-progress");
        progressBar.setPrefHeight(8);

        TextArea logArea = new TextArea();
        logArea.setEditable(false);
        logArea.getStyleClass().add("launcher-update-log");
        logArea.setPrefHeight(160);
        logArea.setFont(Font.font("JetBrains Mono", 11));
        VBox.setVgrow(logArea, Priority.ALWAYS);

        Button uploadBtn = new Button("⬆ Опублікувати на R2 (compare-by-hash)");
        uploadBtn.getStyleClass().add("launcher-update-btn");
        uploadBtn.setMaxWidth(Double.MAX_VALUE);
        uploadBtn.setPrefHeight(42);
        uploadBtn.setOnAction(e -> {
            if (loadedManifest[0] == null || loadedDir[0] == null) {
                alert("Спочатку виберіть теку app-image з готовим launcher-version.json");
                return;
            }

            PackManagerConfig.R2Settings r2cfg = PackManagerConfig.settings().r2();
            if (!r2cfg.isConfigured()) {
                alert("R2 не налаштовано. Спочатку налаштуйте R2 в розділі Налаштування.");
                return;
            }

            JsonObject manifest = loadedManifest[0];
            Path appImageDir = loadedDir[0];
            boolean isShaurma = rbShaurma.isSelected();
            String launcherVariant = isShaurma ? "shaurma" : "base";
            String version = manifest.has("version") ? manifest.get("version").getAsString() : versionField.getText().trim();

            // Дозволяємо відредагувати release notes перед публікацією
            // (не перечитуємо з диска -- беремо те, що зараз у полі).
            manifest.addProperty("releaseNotes", releaseNotesArea.getText());

            uploadBtn.setDisable(true);
            logArea.clear();
            progressBar.setProgress(0);
            progressLabel.setText("Публікація...");

            Task<Void> task = new Task<>() {
                @Override protected Void call() throws Exception {
                    if (!manifest.has("components") || !manifest.get("components").isJsonArray()) {
                        throw new RuntimeException("launcher-version.json не містить масиву \"components\"");
                    }
                    JsonArray components = manifest.getAsJsonArray("components");
                    int total = components.size();
                    updateMessage("Знайдено " + total + " компонентів (версія " + version + ")");

                    try (R2Client r2 = new R2Client(r2cfg)) {
                        // ── 1. Заливаємо кожен компонент за його "path" ──────
                        // Ключ на R2: launchers/<variant>/<component "url" без ведучого "/">
                        // Наприклад url="/launcher/launcher-core.exe" -->
                        //           launchers/shaurma/launcher-core.exe
                        int done = 0;
                        for (JsonElement el : components) {
                            JsonObject comp = el.getAsJsonObject();
                            String relPath = comp.get("path").getAsString();
                            String url     = comp.has("url") ? comp.get("url").getAsString() : ("/launcher/" + relPath);
                            String cleanUrl = url.startsWith("/") ? url.substring(1) : url;
                            // "launcher/<x>" -> "launchers/<variant>/<x>" (той самий
                            // мепінг, що Worker вже застосовує на льоту для
                            // старого installerUrl -- тут робимо явно, бо
                            // заливаємо напряму на R2, без Worker-посередника).
                            String r2Suffix = cleanUrl.startsWith("launcher/")
                                ? cleanUrl.substring("launcher/".length())
                                : cleanUrl;
                            String r2Key = "launchers/" + launcherVariant + "/" + r2Suffix;

                            Path localFile = appImageDir.resolve(relPath.replace('/', File.separatorChar));
                            if (!Files.exists(localFile)) {
                                updateMessage("[ПРОПУЩЕНО] " + relPath + " -- відсутній локально");
                                continue;
                            }

                            updateMessage("Завантаження [" + (done + 1) + "/" + total + "] " + relPath + " -> " + r2Key);
                            R2Client.UploadResult res = r2.upload(localFile, r2Key, null);
                            if (!res.success()) {
                                throw new RuntimeException("Помилка завантаження " + relPath + ": " + res.error());
                            }
                            done++;
                            updateProgress(done, total);
                        }
                        updateMessage("✓ Усі компоненти залито (" + done + "/" + total + ")");

                        // ── 2. Публікуємо сам launcher-version.json останнім ──
                        // Важливо: маніфест йде ОСТАННІМ, щоб клієнти (launcher-core
                        // runUpdateGate) ніколи не побачили нову версію маніфесту
                        // раніше, ніж усі файли, на які він посилається, вже лежать
                        // на CDN -- інакше апдейтер спробує тягнути файл, якого ще
                        // немає, і впаде посеред оновлення.
                        String r2VersionKey = "launchers/" + launcherVariant + "/launcher-version.json";
                        String json = GSON.toJson(manifest);
                        updateMessage("Публікація " + r2VersionKey + "...");
                        R2Client.UploadResult jsonResult = r2.putString(r2VersionKey, json, "application/json");
                        if (!jsonResult.success()) {
                            throw new RuntimeException("Помилка завантаження launcher-version.json: " + jsonResult.error());
                        }
                        updateMessage("✓ [" + launcherVariant.toUpperCase() + "] версія " + version + " опублікована!");
                    }
                    return null;
                }
            };

            task.messageProperty().addListener((obs, o, n) -> {
                Platform.runLater(() -> {
                    logArea.appendText(n + "\n");
                    progressLabel.setText(n);
                });
            });
            task.progressProperty().addListener((obs, o, n) -> {
                Platform.runLater(() -> progressBar.setProgress(n.doubleValue()));
            });
            task.setOnSucceeded(ev -> Platform.runLater(() -> {
                uploadBtn.setDisable(false);
                progressBar.setProgress(1.0);
            }));
            task.setOnFailed(ev -> Platform.runLater(() -> {
                uploadBtn.setDisable(false);
                progressBar.setProgress(0);
                String err = task.getException().getMessage();
                logArea.appendText("[ERROR] " + err + "\n");
                progressLabel.setText("✗ " + err);
            }));

            new Thread(task, "launcher-upload").start();
        });

        uploadSection.getChildren().addAll(progressBar, progressLabel, logArea, uploadBtn);
        card.getChildren().add(uploadSection);

        page.getChildren().addAll(brandRow, card);

        // ── Підпираємо знизу ──
        Region spacer = new Region();
        VBox.setVgrow(spacer, Priority.ALWAYS);
        page.getChildren().add(spacer);

        ScrollPane scroll = new ScrollPane(page);
        scroll.setFitToWidth(true);
        setContent(scroll);
    }

    // ══════════════════════════════════════════════════════════════════════
    //  Settings Screen
    // ══════════════════════════════════════════════════════════════════════

    private void showSettingsScreen() {
        VBox page = new VBox(20);
        page.getStyleClass().add("page");
        page.setPadding(new Insets(24));

        Label title = new Label("Налаштування");
        title.getStyleClass().add("page-title");

        PackManagerConfig.Settings cfg = PackManagerConfig.settings();

        VBox form = new VBox(12);
        form.getStyleClass().add("card");
        form.setPadding(new Insets(16));

        // Data directory
        TextField dataDirField = formField("Шлях до папки з даними", cfg.dataDirectory());
        HBox dataDirRow = new HBox(8, dataDirField, browseDataDir("Вибрати", dataDirField, stage));
        HBox.setHgrow(dataDirField, Priority.ALWAYS);

        // Cache directory
        TextField cacheDirField = formField("Шлях до кешу", cfg.cacheDirectory());
        HBox cacheDirRow = new HBox(8, cacheDirField, browseDir("Вибрати", cacheDirField, stage));
        HBox.setHgrow(cacheDirField, Priority.ALWAYS);

        // Zip compression
        Spinner<Integer> zipSpinner = new Spinner<>(1, 9, cfg.zipCompressionLevel());
        zipSpinner.getStyleClass().add("form-spinner");

        // Archive folders
        TextArea archiveFoldersArea = new TextArea(
            String.join("\n", cfg.archiveFolders()));
        archiveFoldersArea.setPrefRowCount(4);
        archiveFoldersArea.setPromptText("Одна папка на рядок, наприклад: tacz/");
        archiveFoldersArea.getStyleClass().add("form-area");

        CheckBox autoManifest = new CheckBox("Автоматично генерувати manifest після завантаження mrpack");
        autoManifest.setSelected(cfg.autoGenerateManifest());

        Button saveBtn = new Button("💾 Зберегти і запустити Deploy");
        saveBtn.getStyleClass().addAll("btn", "btn-primary", "btn-large");
        saveBtn.setOnAction(e -> {
            try {
                List<String> folders = Arrays.asList(archiveFoldersArea.getText().split("\n"));
                folders.removeIf(String::isBlank);
                String newDataDir = dataDirField.getText();
                PackManagerConfig.updateSettings(new PackManagerConfig.Settings(
                    newDataDir,
                    cacheDirField.getText(),
                    PackManagerConfig.settings().r2(),
                    folders,
                    zipSpinner.getValue(),
                    autoManifest.isSelected()
                ));

                // Авто-детект збірок у dataDirectory
                if (newDataDir != null && !newDataDir.isBlank()) {
                    Path dd = Path.of(newDataDir);
                    if (Files.isDirectory(dd)) {
                        try (var dirs = Files.list(dd)) {
                            dirs.filter(Files::isDirectory).forEach(sub -> {
                                String id = sub.getFileName().toString();
                                if (packs.stream().anyMatch(p -> p.getId().equals(id))) return;
                                boolean hasMods = Files.exists(sub.resolve("mods"));
                                boolean hasConfig = Files.exists(sub.resolve("config"));
                                if (hasMods || hasConfig) {
                                    PackProject p = PackProject.createNew(id, id);
                                    try (var files = Files.list(sub)) {
                                        files.filter(f -> f.toString().endsWith(".mrpack")).findFirst()
                                            .ifPresent(m -> { p.setMrpackPath(m.toString()); p.setMrpackUrl(id + ".mrpack"); });
                                    } catch (IOException ignored) {}
                                    packs.add(p);
                                }
                            });
                        }
                    }
                    saveProject();
                }

                // Авто-деплой — одразу переходимо на екран Deploy
                if (!packs.isEmpty()) {
                    showDeployScreen();
                } else {
                    alert("Налаштування збережено. Не знайдено жодної збірки в папці.");
                }
            } catch (IOException ex) {
                alert("Помилка збереження: " + ex.getMessage());
            }
        });

        form.getChildren().addAll(
            new Label("Налаштування Pack Manager"),
            formRow("Папка з даними:", dataDirRow),
            formRow("Кеш:", cacheDirRow),
            formRow("Стиснення ZIP (1-9):", zipSpinner),
            formRow("Force Archive папки:", archiveFoldersArea),
            autoManifest,
            saveBtn
        );

        page.getChildren().addAll(title, form);
        ScrollPane scroll = new ScrollPane(page);
        scroll.setFitToWidth(true);
        setContent(scroll);
    }

    // ══════════════════════════════════════════════════════════════════════
    //  Діалоги
    // ══════════════════════════════════════════════════════════════════════

    private void showNewPackDialog() {
        Dialog<PackProject> dialog = new Dialog<>();
        dialog.setTitle("Нова збірка");
        dialog.setHeaderText("Введіть дані нової збірки");
        dialog.getDialogPane().getStylesheets().add(
            getClass().getResource("/css/pack-manager.css").toExternalForm());

        TextField idField   = formField("id", "my-pack");
        TextField nameField = formField("Назва", "My Modpack");

        VBox form = new VBox(12,
            formRow("ID:", idField),
            formRow("Назва:", nameField));
        form.setPadding(new Insets(16));
        dialog.getDialogPane().setContent(form);

        ButtonType createType = new ButtonType("Створити", ButtonBar.ButtonData.OK_DONE);
        dialog.getDialogPane().getButtonTypes().addAll(createType, ButtonType.CANCEL);

        dialog.setResultConverter(bt -> {
            if (bt == createType) {
                PackProject p = PackProject.createNew(idField.getText(), nameField.getText());
                return p;
            }
            return null;
        });

        dialog.showAndWait().ifPresent(pack -> {
            // Перевіряємо чи немає збірки з таким ID
            boolean exists = packs.stream()
                .anyMatch(p -> pack.getId() != null && pack.getId().equals(p.getId()));
            if (exists) {
                alert("Збірка з ID '" + pack.getId() + "' вже існує!");
                return;
            }
            packs.add(pack);
            saveLocalPaths(); // Тільки локальні шляхи одразу
            saveLocalPacksMetadata(); // також метадані — щоб не загубити при першому запуску
            showEditPack(pack);
        });
    }

    private void showAddRuleDialog(PackProject pack, TableView<Map.Entry<String, String>> table) {
        Dialog<Map.Entry<String, String>> dialog = new Dialog<>();
        dialog.setTitle("Додати правило");

        TextField pathField = formField("Шлях", "myFolder/");
        ComboBox<String> ruleCombo = new ComboBox<>(
            FXCollections.observableArrayList(PackProject.ALL_RULES));
        ruleCombo.setValue(PackProject.RULE_SKIP);

        VBox form = new VBox(12,
            formRow("Шлях:", pathField),
            formRow("Правило:", ruleCombo),
            new Label(PackProject.ruleDescription(ruleCombo.getValue())));
        form.setPadding(new Insets(16));
        dialog.getDialogPane().setContent(form);

        ButtonType addType = new ButtonType("Додати", ButtonBar.ButtonData.OK_DONE);
        dialog.getDialogPane().getButtonTypes().addAll(addType, ButtonType.CANCEL);

        dialog.setResultConverter(bt -> {
            if (bt == addType) return Map.entry(pathField.getText(), ruleCombo.getValue());
            return null;
        });

        dialog.showAndWait().ifPresent(entry -> {
            pack.setSyncRule(entry.getKey(), entry.getValue());
            table.setItems(FXCollections.observableArrayList(pack.getSyncRules().entrySet()));
        });
    }

    private void showEditRuleDialog(PackProject pack, String path,
                                     TableView<Map.Entry<String, String>> table) {
        Dialog<String> dialog = new Dialog<>();
        dialog.setTitle("Редагування правила: " + path);

        ComboBox<String> ruleCombo = new ComboBox<>(
            FXCollections.observableArrayList(PackProject.ALL_RULES));
        ruleCombo.setValue(pack.getSyncRules().getOrDefault(path, PackProject.RULE_SKIP));

        Label desc = new Label(PackProject.ruleDescription(ruleCombo.getValue()));
        desc.setWrapText(true);
        ruleCombo.setOnAction(e -> desc.setText(PackProject.ruleDescription(ruleCombo.getValue())));

        VBox form = new VBox(12,
            new Label("Шлях: " + path),
            formRow("Правило:", ruleCombo),
            desc);
        form.setPadding(new Insets(16));
        dialog.getDialogPane().setContent(form);

        ButtonType saveType = new ButtonType("Зберегти", ButtonBar.ButtonData.OK_DONE);
        dialog.getDialogPane().getButtonTypes().addAll(saveType, ButtonType.CANCEL);

        dialog.setResultConverter(bt -> bt == saveType ? ruleCombo.getValue() : null);

        dialog.showAndWait().ifPresent(rule -> {
            pack.setSyncRule(path, rule);
            table.setItems(FXCollections.observableArrayList(pack.getSyncRules().entrySet()));
        });
    }

    // ══════════════════════════════════════════════════════════════════════
    //  Persistence
    // ══════════════════════════════════════════════════════════════════════

    /**
     * Завантажує список збірок із сервера (server-manifest.json на R2).
     * Локально зберігається тільки local-paths.json — шляхи до .mrpack, icon, background.
     */
    private void loadProjectFromDisk() {
        Path dir = PackManagerConfig.workDir();
        // projectFile — локальний кеш для локальних шляхів
        projectFile = dir.resolve("local-paths.json");

        // Спробувати завантажити з R2
        PackManagerConfig.R2Settings r2cfg = PackManagerConfig.settings().r2();
        if (r2cfg.isConfigured()) {
            try (R2Client r2 = new R2Client(r2cfg)) {
                String json = r2.getString("server-manifest.json");
                if (json != null) {
                    // server-manifest.json містить ServerManifest структуру
                    // Конвертуємо в PackProject список
                    com.google.gson.JsonObject root = GSON.fromJson(json, com.google.gson.JsonObject.class);
                    com.google.gson.JsonArray packsArr = root.has("packs")
                        ? root.getAsJsonArray("packs") : new com.google.gson.JsonArray();

                    // Читаємо локальні шляхи
                    java.util.Map<String, LocalPaths> localPaths = loadLocalPaths();

                    packs.clear();
                    for (com.google.gson.JsonElement el : packsArr) {
                        com.google.gson.JsonObject obj = el.getAsJsonObject();
                        PackProject p = new PackProject();
                        p.setId(getString(obj, "id"));
                        p.setName(getString(obj, "name"));
                        p.setMcVersion(getString(obj, "mcVersion"));
                        p.setLoader(getString(obj, "loader"));
                        p.setLoaderVersion(getString(obj, "loaderVersion"));
                        p.setDescription(getString(obj, "description"));
                        if (obj.has("tags")) {
                            java.util.List<String> tags = new java.util.ArrayList<>();
                            obj.getAsJsonArray("tags").forEach(t -> tags.add(t.getAsString()));
                            p.setTags(tags);
                        }
                        // mrpackPath — відносний R2 ключ (поле mrpackPath в маніфесті)
                        String manifestMrpackPath = getString(obj, "mrpackPath");
                        if (manifestMrpackPath != null) p.setMrpackUrl(manifestMrpackPath);
                        p.setServerIp(getString(obj, "serverIp"));
                        // syncRules
                        if (obj.has("syncRules") && !obj.get("syncRules").isJsonNull()) {
                            java.util.Map<String, String> rules = new java.util.LinkedHashMap<>();
                            obj.getAsJsonObject("syncRules").entrySet()
                                .forEach(e2 -> rules.put(e2.getKey(), e2.getValue().getAsString()));
                            p.getSyncRules().clear();
                            p.getSyncRules().putAll(rules);
                        }
                        // lastDeployed
                        String updatedAt = getString(obj, "updatedAt");
                        if (updatedAt != null) p.setLastDeployed(updatedAt);
                        p.markClean();

                        // Відновлюємо локальні шляхи
                        LocalPaths lp = localPaths.get(p.getId());
                        if (lp != null) {
                            p.setMrpackPath(lp.mrpackPath);
                            p.setIconPath(lp.iconPath);
                            p.setBackgroundPath(lp.backgroundPath);
                        }
                        packs.add(p);
                    }
                    log.info("Завантажено {} збірок із R2 server-manifest.json", packs.size());
                    return;
                }
            } catch (Exception e) {
                log.warn("Не вдалось завантажити з R2: {}", e.getMessage());
            }
        }

        // Fallback: локальний packs.json (старий формат, для міграції)
        Path legacyFile = PackManagerConfig.workDir().resolve("packs.json");
        if (Files.exists(legacyFile)) {
            try {
                String json = Files.readString(legacyFile);
                PackProject[] loaded = GSON.fromJson(json, PackProject[].class);
                if (loaded != null) packs.addAll(java.util.Arrays.asList(loaded));
                log.info("Завантажено {} збірок з локального packs.json (legacy)", packs.size());
            } catch (Exception e) {
                log.warn("Помилка читання packs.json: {}", e.getMessage());
            }
        }
    }

    private String getString(com.google.gson.JsonObject obj, String key) {
        return obj.has(key) && !obj.get(key).isJsonNull() ? obj.get(key).getAsString() : null;
    }

    private static class LocalPaths {
        String mrpackPath;
        String iconPath;
        String backgroundPath;
    }

    /**
     * Зберігає метадані всіх збірок локально (packs.json в workDir).
     * Потрібно для першого запуску: поки R2 порожній, дані не губляться
     * при перезапуску Pack Manager до деплою.
     */
    private void saveLocalPacksMetadata() {
        try {
            Path packsFile = PackManagerConfig.workDir().resolve("packs.json");
            Files.createDirectories(packsFile.getParent());
            Files.writeString(packsFile, GSON.toJson(new java.util.ArrayList<>(packs)));
            log.info("Збережено {} збірок локально: {}", packs.size(), packsFile);
        } catch (IOException e) {
            log.error("Помилка збереження packs.json: {}", e.getMessage());
        }
    }

    private java.util.Map<String, LocalPaths> loadLocalPaths() {
        try {
            if (Files.exists(projectFile)) {
                String json = Files.readString(projectFile);
                java.lang.reflect.Type t = new com.google.gson.reflect.TypeToken<
                    java.util.Map<String, LocalPaths>>(){}.getType();
                java.util.Map<String, LocalPaths> map = GSON.fromJson(json, t);
                return map != null ? map : new java.util.LinkedHashMap<>();
            }
        } catch (Exception e) {
            log.warn("Помилка читання local-paths.json: {}", e.getMessage());
        }
        return new java.util.LinkedHashMap<>();
    }

    private void saveLocalPaths() {
        try {
            Files.createDirectories(projectFile.getParent());
            java.util.Map<String, LocalPaths> map = new java.util.LinkedHashMap<>();
            for (PackProject p : packs) {
                LocalPaths lp = new LocalPaths();
                lp.mrpackPath      = p.getMrpackPath();
                lp.iconPath        = p.getIconPath();
                lp.backgroundPath  = p.getBackgroundPath();
                map.put(p.getId(), lp);
            }
            Files.writeString(projectFile, GSON.toJson(map));
        } catch (IOException e) {
            log.error("Помилка збереження local-paths.json: {}", e.getMessage());
        }
    }

    /**
     * "Зберегти" — ЛОКАЛЬНЕ збереження збірки (local-paths.json + метадані).
     * Публікація на R2 тепер лише через явний Deploy-екран (DeployServiceV2):
     * раніше «Зберегти» тихо перезаписувало per-pack маніфест старим
     * V1-форматом (ManifestGenerator), псуючи актуальні дані на проді
     * незалежно від того, коли оператор востаннє деплоїв через V2.
     * Збірка лишається «dirty» (⬤), поки не задеплоєна — Deploy-екран
     * обере її за замовчуванням.
     */
    private void saveProject() {
        saveLocalPaths();
        saveLocalPacksMetadata();
    }

    // ══════════════════════════════════════════════════════════════════════
    //  Helper методи для UI
    // ══════════════════════════════════════════════════════════════════════

    // quickDeploy — deploy ОДНІЄЇ збірки: відкриває Deploy-екран,
    // зосереджений на цьому паку (раніше параметр pack ІГНОРУВАВСЯ і метод
    // просто перемикав на загальний екран — сигнатура обіцяла одне,
    // робила інше).
    private void quickDeploy(PackProject pack) {
        showDeployScreen(pack);
    }

    private HBox formRow(String label, javafx.scene.Node field) {
        Label lbl = new Label(label);
        lbl.getStyleClass().add("form-label");
        lbl.setMinWidth(160);
        HBox row = new HBox(12, lbl, field);
        row.setAlignment(Pos.CENTER_LEFT);
        HBox.setHgrow(field, Priority.ALWAYS);
        return row;
    }

    private TextField formField(String prompt, String value) {
        TextField tf = new TextField(value != null ? value : "");
        tf.setPromptText(prompt);
        tf.getStyleClass().add("form-field");
        return tf;
    }

    @FunctionalInterface
    private interface FieldSetter<T> {
        void accept(T value);
    }

    private TextArea packTextArea(String value, FieldSetter<String> setter) {
        TextArea ta = new TextArea(value != null ? value : "");
        ta.getStyleClass().add("form-area");
        ta.setPrefRowCount(3);
        ta.textProperty().addListener((obs, o, n) -> setter.accept(n));
        return ta;
    }

    private TextField packField(String value, FieldSetter<String> setter) {
        TextField tf = formField("", value);
        tf.textProperty().addListener((obs, o, n) -> setter.accept(n));
        return tf;
    }

    private HBox buildFilePickerRow(String currentValue, FieldSetter<String> setter,
                                      String dialogTitle,
                                      javafx.stage.FileChooser.ExtensionFilter filter) {
        TextField tf = formField("", currentValue);
        tf.textProperty().addListener((obs, o, n) -> setter.accept(n.isBlank() ? null : n));
        HBox.setHgrow(tf, Priority.ALWAYS);

        Button btn = new Button("📁");
        btn.getStyleClass().addAll("btn", "btn-default");
        btn.setOnAction(e -> {
            FileChooser fc = new FileChooser();
            fc.setTitle(dialogTitle);
            fc.getExtensionFilters().add(filter);
            FileDialogMemory.applyTo(fc);
            File f = fc.showOpenDialog(stage);
            FileDialogMemory.rememberParentOf(f);
            if (f != null) {
                tf.setText(f.getAbsolutePath());
                setter.accept(f.getAbsolutePath());
                saveLocalPaths(); // одразу зберігаємо після вибору файлу
            }
        });

        return new HBox(8, tf, btn);
    }

    private Button browseDir(String label, TextField target, Stage stage) {
        Button btn = new Button("📁 " + label);
        btn.getStyleClass().addAll("btn", "btn-default");
        btn.setOnAction(e -> {
            DirectoryChooser dc = new DirectoryChooser();
            dc.setTitle("Виберіть директорію");
            FileDialogMemory.applyTo(dc);
            File f = dc.showDialog(stage);
            FileDialogMemory.rememberParentOf(f);
            if (f != null) target.setText(f.getAbsolutePath());
        });
        return btn;
    }

    private Button browseDataDir(String label, TextField target, Stage stage) {
        Button btn = new Button("📁 " + label);
        btn.getStyleClass().addAll("btn", "btn-default");
        btn.setOnAction(e -> {
            DirectoryChooser dc = new DirectoryChooser();
            dc.setTitle("Виберіть папку з даними");
            FileDialogMemory.applyTo(dc);
            File f = dc.showDialog(stage);
            FileDialogMemory.rememberParentOf(f);
            if (f != null) {
                target.setText(f.getAbsolutePath());
                try {
                    PackManagerConfig.updateSettings(new PackManagerConfig.Settings(
                        target.getText(),
                        PackManagerConfig.settings().cacheDirectory(),
                        PackManagerConfig.settings().r2(),
                        PackManagerConfig.settings().archiveFolders(),
                        PackManagerConfig.settings().zipCompressionLevel(),
                        PackManagerConfig.settings().autoGenerateManifest()
                    ));
                } catch (IOException ex) {
                    log.error("Не вдалось зберегти dataDirectory: {}", ex.getMessage());
                }
            }
        });
        return btn;
    }

    private void alert(String msg) {
        Alert a = new Alert(Alert.AlertType.INFORMATION, msg, ButtonType.OK);
        a.showAndWait();
    }

    private static String formatBytes(long b) {
        if (b < 1024)       return b + " B";
        if (b < 1048576)    return String.format("%.1f KB", b / 1024.0);
        if (b < 1073741824) return String.format("%.1f MB", b / 1048576.0);
        return String.format("%.2f GB", b / 1073741824.0);
    }

}
