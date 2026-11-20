package dev.shaurma.agent;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.lang.management.GarbageCollectorMXBean;
import java.lang.management.ManagementFactory;
import java.lang.management.MemoryMXBean;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

/**
 * Локальний керуючий інтерфейс агента: HTTP POST /cmd на 127.0.0.1 із токеном. Ним користується лише лаунчер
 * (для ШІ-агента). Браузерні запити (заголовок Origin) і чужі Host відхиляються.
 */
final class Control {
    private static final int MAX_BODY = 256 * 1024;
    private final int port;
    private final String token;
    private final String nick;
    private ServerSocket server;

    Control(int port, String token, String nick) {
        this.port = port;
        this.token = token;
        this.nick = nick;
    }

    int start() throws IOException {
        server = new ServerSocket(port, 8, InetAddress.getByName("127.0.0.1"));
        Thread t = new Thread(new Runnable() {
            @Override
            public void run() {
                while (!server.isClosed()) {
                    try {
                        final Socket s = server.accept();
                        s.setSoTimeout(15000);
                        Thread w = new Thread(new Runnable() {
                            @Override
                            public void run() {
                                handle(s);
                            }
                        }, "shaurma-agent-conn");
                        w.setDaemon(true);
                        w.start();
                    } catch (IOException e) {
                        if (server.isClosed()) return;
                    }
                }
            }
        }, "shaurma-agent-accept");
        t.setDaemon(true);
        t.start();
        return server.getLocalPort();
    }

    // ── HTTP ─────────────────────────────────────────────────────────────

    private void handle(Socket s) {
        try (Socket sock = s) {
            InputStream in = sock.getInputStream();
            String head = readHead(in);
            if (head == null) return;
            String[] lines = head.split("\r\n");
            String[] rl = lines[0].split(" ");
            Map<String, String> h = new LinkedHashMap<String, String>();
            for (int i = 1; i < lines.length; i++) {
                int c = lines[i].indexOf(':');
                if (c > 0) h.put(lines[i].substring(0, c).trim().toLowerCase(Locale.ROOT), lines[i].substring(c + 1).trim());
            }
            if (h.containsKey("origin")) {
                reply(sock, 403, err("forbidden"));
                return;
            }
            String host = h.get("host");
            if (host == null || !(host.equals("127.0.0.1:" + server.getLocalPort()) || host.equals("localhost:" + server.getLocalPort()))) {
                reply(sock, 403, err("forbidden"));
                return;
            }
            if (!MessageDigest.isEqual(String.valueOf(h.get("x-shaurma-token")).getBytes(StandardCharsets.UTF_8), token.getBytes(StandardCharsets.UTF_8))) {
                reply(sock, 401, err("unauthorized"));
                return;
            }
            if (rl.length < 2 || !rl[0].equals("POST") || !rl[1].equals("/cmd")) {
                reply(sock, 404, err("not found"));
                return;
            }
            int len = 0;
            try {
                len = Integer.parseInt(h.getOrDefault("content-length", "0"));
            } catch (NumberFormatException ignored) {
            }
            if (len < 0 || len > MAX_BODY) {
                reply(sock, 413, err("too large"));
                return;
            }
            byte[] body = new byte[len];
            int off = 0;
            while (off < len) {
                int r = in.read(body, off, len - off);
                if (r < 0) break;
                off += r;
            }
            Map<String, Object> resp;
            try {
                resp = dispatch(Json.parseObject(new String(body, 0, off, StandardCharsets.UTF_8)));
            } catch (IllegalArgumentException e) {
                resp = fail("некоректний запит: " + e.getMessage());
            } catch (Throwable t) {
                resp = fail("помилка агента: " + t);
            }
            reply(sock, 200, resp);
        } catch (Throwable ignored) {
            // клієнт відключився
        }
    }

    private static String readHead(InputStream in) throws IOException {
        ByteArrayOutputStream b = new ByteArrayOutputStream();
        int last4 = 0;
        while (b.size() < 16 * 1024) {
            int c = in.read();
            if (c < 0) return null;
            b.write(c);
            last4 = (last4 << 8) | c;
            if (last4 == 0x0d0a0d0a) return new String(b.toByteArray(), StandardCharsets.ISO_8859_1).trim();
        }
        return null;
    }

    private static Map<String, Object> err(String m) {
        return fail(m);
    }

    private static void reply(Socket s, int code, Map<String, Object> body) throws IOException {
        byte[] data = Json.write(body).getBytes(StandardCharsets.UTF_8);
        String status = code == 200 ? "OK" : code == 401 ? "Unauthorized" : code == 403 ? "Forbidden" : code == 404 ? "Not Found" : "Error";
        OutputStream o = s.getOutputStream();
        o.write(("HTTP/1.1 " + code + " " + status + "\r\nContent-Type: application/json; charset=utf-8\r\nContent-Length: " + data.length
                + "\r\nConnection: close\r\n\r\n").getBytes(StandardCharsets.ISO_8859_1));
        o.write(data);
        o.flush();
    }

    // ── Команди ──────────────────────────────────────────────────────────

    static Map<String, Object> ok() {
        Map<String, Object> m = new LinkedHashMap<String, Object>();
        m.put("ok", Boolean.TRUE);
        return m;
    }

    static Map<String, Object> fail(String msg) {
        Map<String, Object> m = new LinkedHashMap<String, Object>();
        m.put("ok", Boolean.FALSE);
        m.put("error", msg);
        return m;
    }

    private static String str(Map<String, Object> m, String k, String def) {
        Object v = m.get(k);
        return v == null ? def : String.valueOf(v);
    }

    private static double num(Map<String, Object> m, String k, double def) {
        Object v = m.get(k);
        return v instanceof Number ? ((Number) v).doubleValue() : def;
    }

    Map<String, Object> dispatch(Map<String, Object> req) throws Exception {
        String cmd = str(req, "cmd", "");
        Hooks hk = Hooks.I;
        switch (cmd) {
            case "ping": {
                Map<String, Object> r = ok();
                r.put("agent", "shaurma-game-agent");
                r.put("version", Agent.VERSION);
                r.put("nick", nick);
                return r;
            }
            case "status":
                return status();
            case "screenshot":
                return screenshot(req);
            case "key":
                return key(req);
            case "type":
                return type(req);
            case "chat":
                return chat(req);
            case "mouse_move":
                return mouseMove(req);
            case "mouse_click":
                return mouseClick(req);
            case "scroll": {
                if (!hk.ready()) return notReady();
                final double dx = num(req, "dx", 0), dy = num(req, "dy", 0);
                List<long[]> d = new ArrayList<long[]>();
                List<Runnable> s = new ArrayList<Runnable>();
                d.add(new long[]{0});
                s.add(new Runnable() {
                    public void run() {
                        Hooks.I.scroll(dx, dy);
                    }
                });
                return finish(hk.runSteps(d, s, 3000));
            }
            case "keep_focus":
                hk.keepFocus = !Boolean.FALSE.equals(req.get("enabled"));
                hk.focusSent = false;
                return ok();
            default:
                return fail("невідома команда: " + cmd);
        }
    }

    private static Map<String, Object> notReady() {
        return fail("хуки вводу ще не готові: гра завантажується або не малює кадри (згорнуто?). Повторіть за кілька секунд.");
    }

    private static Map<String, Object> finish(boolean done) {
        return done ? ok() : fail("команда не виконалась: гра не малює кадри (вікно згорнуто або гра зависла)");
    }

    private Map<String, Object> status() {
        Hooks hk = Hooks.I;
        Map<String, Object> r = ok();
        r.put("agent_version", Agent.VERSION);
        r.put("nick", nick);
        r.put("input_ready", hk.ready());
        r.put("cursor_grabbed", hk.cursorDisabled);
        r.put("window", mapOf("width", hk.winW, "height", hk.winH, "framebuffer_width", hk.fbW, "framebuffer_height", hk.fbH));
        r.put("fps", hk.fpsStats());
        r.put("uptime_sec", (System.nanoTime() - hk.startedNanos) / 1_000_000_000L);

        MemoryMXBean mem = ManagementFactory.getMemoryMXBean();
        Map<String, Object> jvm = new LinkedHashMap<String, Object>();
        jvm.put("heap_used_mb", mem.getHeapMemoryUsage().getUsed() >> 20);
        jvm.put("heap_max_mb", mem.getHeapMemoryUsage().getMax() >> 20);
        jvm.put("non_heap_used_mb", mem.getNonHeapMemoryUsage().getUsed() >> 20);
        jvm.put("threads", ManagementFactory.getThreadMXBean().getThreadCount());
        jvm.put("loaded_classes", ManagementFactory.getClassLoadingMXBean().getLoadedClassCount());
        long gcCount = 0, gcMs = 0;
        for (GarbageCollectorMXBean g : ManagementFactory.getGarbageCollectorMXBeans()) {
            gcCount += Math.max(0, g.getCollectionCount());
            gcMs += Math.max(0, g.getCollectionTime());
        }
        jvm.put("gc_count", gcCount);
        jvm.put("gc_time_ms", gcMs);
        jvm.put("java", System.getProperty("java.version"));
        try {
            java.lang.management.OperatingSystemMXBean os = ManagementFactory.getOperatingSystemMXBean();
            if (os instanceof com.sun.management.OperatingSystemMXBean) {
                com.sun.management.OperatingSystemMXBean x = (com.sun.management.OperatingSystemMXBean) os;
                jvm.put("process_cpu_percent", Math.round(x.getProcessCpuLoad() * 1000) / 10.0);
                jvm.put("system_cpu_percent", Math.round(x.getSystemCpuLoad() * 1000) / 10.0);
            }
        } catch (Throwable ignored) {
        }
        r.put("jvm", jvm);
        r.put("mods", Mods.list(hk.cbKey != null ? hk.cbKey : hk.cbMouse, hk.glfw));
        r.put("network_guard", mapOf("blocked_connections", Net.BLOCKED.get(), "last_blocked", Net.lastBlocked));
        if (!hk.lastError.isEmpty()) r.put("last_internal_error", hk.lastError);
        return r;
    }

    private Map<String, Object> screenshot(Map<String, Object> req) throws Exception {
        Hooks hk = Hooks.I;
        if (hk.glfw == null || hk.window == 0) return notReady();
        int maxW = (int) num(req, "max_width", 960);
        String fmt = str(req, "format", "png").toLowerCase(Locale.ROOT);
        Capture.Request cr = new Capture.Request();
        hk.pendingShot = cr;
        if (!cr.await(8000)) {
            hk.pendingShot = null;
            return fail("кадр не отримано за 8 с: гра не малює кадри (вікно згорнуто?)");
        }
        if (cr.error != null) return fail(cr.error);
        byte[] img = Capture.encode(cr.w, cr.h, cr.rgb, Math.max(0, maxW), fmt);
        Map<String, Object> r = ok();
        r.put("mime", fmt.startsWith("j") ? "image/jpeg" : "image/png");
        r.put("width", cr.w);
        r.put("height", cr.h);
        r.put("data_base64", Base64.getEncoder().encodeToString(img));
        return r;
    }

    private Map<String, Object> key(Map<String, Object> req) throws Exception {
        Hooks hk = Hooks.I;
        if (!hk.ready()) return notReady();
        final int code = Keys.code(str(req, "key", ""));
        if (code < 0) return fail("невідома клавіша: " + str(req, "key", "") + " (напр. w, space, escape, f3, left_shift, enter, tab)");
        final int mods = Keys.mods(req.get("mods") instanceof List ? (List<?>) req.get("mods") : null);
        String action = str(req, "action", "tap");
        long hold = (long) Math.max(0, Math.min(60000, num(req, "hold_ms", 70)));
        List<long[]> d = new ArrayList<long[]>();
        List<Runnable> s = new ArrayList<Runnable>();
        if (action.equals("press") || action.equals("tap")) {
            d.add(new long[]{0});
            s.add(new Runnable() { public void run() { Hooks.I.key(code, 1, mods); } });
        }
        if (action.equals("release") || action.equals("tap")) {
            d.add(new long[]{action.equals("tap") ? hold : 0});
            s.add(new Runnable() { public void run() { Hooks.I.key(code, 0, mods); } });
        }
        if (s.isEmpty()) return fail("action: tap | press | release");
        return finish(hk.runSteps(d, s, 3000));
    }

    private Map<String, Object> type(Map<String, Object> req) throws Exception {
        Hooks hk = Hooks.I;
        if (!hk.ready()) return notReady();
        String text = str(req, "text", "");
        if (text.length() > 500) return fail("текст завдовгий (макс. 500 символів)");
        long delay = (long) Math.max(5, Math.min(500, num(req, "delay_ms", 25)));
        List<long[]> d = new ArrayList<long[]>();
        List<Runnable> s = new ArrayList<Runnable>();
        long t = 0;
        for (int i = 0; i < text.length(); ) {
            final int cp = text.codePointAt(i);
            i += Character.charCount(cp);
            d.add(new long[]{t});
            s.add(new Runnable() { public void run() { Hooks.I.chr(cp); } });
            t += delay;
        }
        return finish(hk.runSteps(d, s, 4000));
    }

    /** Чат: відкрити (T або «/»), ввести текст, Enter. Команди — з ведучим «/». */
    private Map<String, Object> chat(Map<String, Object> req) throws Exception {
        Hooks hk = Hooks.I;
        if (!hk.ready()) return notReady();
        String text = str(req, "text", "");
        if (text.isEmpty() || text.length() > 256) return fail("text: 1..256 символів");
        boolean command = text.startsWith("/");
        final int openKey = command ? Keys.code("slash") : Keys.code(str(req, "open_key", "t"));
        if (openKey < 0) return fail("невідома open_key");
        String body = command ? text.substring(1) : text;
        List<long[]> d = new ArrayList<long[]>();
        List<Runnable> s = new ArrayList<Runnable>();
        long t = 0;
        d.add(new long[]{t});
        s.add(new Runnable() { public void run() { Hooks.I.key(openKey, 1, 0); } });
        t += 60;
        d.add(new long[]{t});
        s.add(new Runnable() { public void run() { Hooks.I.key(openKey, 0, 0); } });
        t += 250; // дати чату відкритись
        for (int i = 0; i < body.length(); ) {
            final int cp = body.codePointAt(i);
            i += Character.charCount(cp);
            d.add(new long[]{t});
            s.add(new Runnable() { public void run() { Hooks.I.chr(cp); } });
            t += 25;
        }
        t += 120;
        final int enter = Keys.code("enter");
        d.add(new long[]{t});
        s.add(new Runnable() { public void run() { Hooks.I.key(enter, 1, 0); } });
        t += 50;
        d.add(new long[]{t});
        s.add(new Runnable() { public void run() { Hooks.I.key(enter, 0, 0); } });
        return finish(hk.runSteps(d, s, 4000));
    }

    private Map<String, Object> mouseMove(Map<String, Object> req) throws Exception {
        Hooks hk = Hooks.I;
        if (!hk.ready()) return notReady();
        List<long[]> d = new ArrayList<long[]>();
        List<Runnable> s = new ArrayList<Runnable>();
        if (req.containsKey("x") || req.containsKey("y")) {
            final double x = num(req, "x", 0), y = num(req, "y", 0);
            d.add(new long[]{0});
            s.add(new Runnable() { public void run() { Hooks.I.cursor(x, y, false); } });
        } else {
            double dx = num(req, "dx", 0), dy = num(req, "dy", 0);
            if (Math.abs(dx) > 20000 || Math.abs(dy) > 20000) return fail("зсув завеликий (макс. 20000 пікселів)");
            // великі повороти — малими кроками (по одному на кадр), як справжня миша
            int steps = (int) Math.max(1, Math.ceil(Math.max(Math.abs(dx), Math.abs(dy)) / 80.0));
            steps = Math.min(steps, 250);
            for (int i = 0; i < steps; i++) {
                final double sx = dx / steps, sy = dy / steps;
                d.add(new long[]{i * 12L});
                s.add(new Runnable() { public void run() { Hooks.I.cursor(sx, sy, true); } });
            }
        }
        return finish(hk.runSteps(d, s, 4000));
    }

    private Map<String, Object> mouseClick(Map<String, Object> req) throws Exception {
        Hooks hk = Hooks.I;
        if (!hk.ready()) return notReady();
        final int btn = Keys.mouseButton(str(req, "button", "left"));
        if (btn < 0) return fail("button: left | right | middle");
        String action = str(req, "action", "tap");
        long hold = (long) Math.max(0, Math.min(60000, num(req, "hold_ms", 70)));
        List<long[]> d = new ArrayList<long[]>();
        List<Runnable> s = new ArrayList<Runnable>();
        long t = 0;
        if (req.containsKey("x") && req.containsKey("y")) {
            final double x = num(req, "x", 0), y = num(req, "y", 0);
            d.add(new long[]{0});
            s.add(new Runnable() { public void run() { Hooks.I.cursor(x, y, false); } });
            t = 30;
        }
        if (action.equals("press") || action.equals("tap")) {
            d.add(new long[]{t});
            s.add(new Runnable() { public void run() { Hooks.I.mouseButton(btn, 1, 0); } });
        }
        if (action.equals("release") || action.equals("tap")) {
            d.add(new long[]{t + (action.equals("tap") ? hold : 0)});
            s.add(new Runnable() { public void run() { Hooks.I.mouseButton(btn, 0, 0); } });
        }
        if (s.isEmpty() || (!action.equals("tap") && !action.equals("press") && !action.equals("release"))) return fail("action: tap | press | release");
        return finish(hk.runSteps(d, s, 3000));
    }

    static Map<String, Object> mapOf(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<String, Object>();
        for (int i = 0; i + 1 < kv.length; i += 2) m.put(String.valueOf(kv[i]), kv[i + 1]);
        return m;
    }
}
