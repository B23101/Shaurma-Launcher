package dev.shaurma.agent;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.InetAddress;
import java.net.Socket;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;

import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

class ControlTest {
    static final String TOKEN = "test_token_0123456789abcdef";
    static int port;

    @BeforeAll
    static void start() throws Exception {
        port = new Control(0, TOKEN, "Claude").start();
    }

    @AfterAll
    static void done() {
        Hooks.I.keepFocus = true;
    }

    private static int[] post(String token, String body, Map<String, String> extraHeaders, StringBuilder out) throws Exception {
        HttpURLConnection c = (HttpURLConnection) new URL("http://127.0.0.1:" + port + "/cmd").openConnection();
        c.setRequestMethod("POST");
        c.setDoOutput(true);
        if (token != null) c.setRequestProperty("X-Shaurma-Token", token);
        if (extraHeaders != null) for (Map.Entry<String, String> e : extraHeaders.entrySet()) c.setRequestProperty(e.getKey(), e.getValue());
        c.getOutputStream().write(body.getBytes(StandardCharsets.UTF_8));
        int code = c.getResponseCode();
        InputStream in = code >= 400 ? c.getErrorStream() : c.getInputStream();
        ByteArrayOutputStream b = new ByteArrayOutputStream();
        byte[] buf = new byte[4096];
        int n;
        while (in != null && (n = in.read(buf)) > 0) b.write(buf, 0, n);
        out.append(new String(b.toByteArray(), StandardCharsets.UTF_8));
        return new int[]{code};
    }

    @Test
    void rejectsMissingOrWrongToken() throws Exception {
        StringBuilder o = new StringBuilder();
        assertEquals(401, post(null, "{\"cmd\":\"ping\"}", null, o)[0]);
        assertEquals(401, post("wrong", "{\"cmd\":\"ping\"}", null, new StringBuilder())[0]);
    }

    @Test
    void rejectsBrowserOrigin() throws Exception {
        // Origin у HttpURLConnection обмежений для заборонених заголовків — шлемо сирим сокетом
        try (Socket s = new Socket(InetAddress.getByName("127.0.0.1"), port)) {
            String body = "{\"cmd\":\"ping\"}";
            String req = "POST /cmd HTTP/1.1\r\nHost: 127.0.0.1:" + port + "\r\nOrigin: https://evil.example\r\nX-Shaurma-Token: " + TOKEN
                    + "\r\nContent-Length: " + body.length() + "\r\nConnection: close\r\n\r\n" + body;
            OutputStream o = s.getOutputStream();
            o.write(req.getBytes(StandardCharsets.UTF_8));
            o.flush();
            byte[] all = new byte[2048];
            int n = s.getInputStream().read(all);
            assertTrue(new String(all, 0, n, StandardCharsets.UTF_8).startsWith("HTTP/1.1 403"));
        }
    }

    @Test
    void rejectsForeignHostHeader() throws Exception {
        try (Socket s = new Socket(InetAddress.getByName("127.0.0.1"), port)) {
            String req = "POST /cmd HTTP/1.1\r\nHost: evil.example\r\nX-Shaurma-Token: " + TOKEN + "\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}";
            s.getOutputStream().write(req.getBytes(StandardCharsets.UTF_8));
            byte[] all = new byte[2048];
            int n = s.getInputStream().read(all);
            assertTrue(new String(all, 0, n, StandardCharsets.UTF_8).startsWith("HTTP/1.1 403"));
        }
    }

    @Test
    void pingStatusAndErrors() throws Exception {
        StringBuilder o = new StringBuilder();
        assertEquals(200, post(TOKEN, "{\"cmd\":\"ping\"}", null, o)[0]);
        Map<String, Object> r = Json.parseObject(o.toString());
        assertEquals(Boolean.TRUE, r.get("ok"));
        assertEquals("Claude", r.get("nick"));

        o.setLength(0);
        post(TOKEN, "{\"cmd\":\"status\"}", null, o);
        r = Json.parseObject(o.toString());
        assertEquals(Boolean.TRUE, r.get("ok"));
        assertNotNull(r.get("jvm"));
        assertNotNull(r.get("network_guard"));
        assertNotNull(r.get("fps"));

        o.setLength(0);
        post(TOKEN, "{\"cmd\":\"bogus\"}", null, o);
        assertEquals(Boolean.FALSE, Json.parseObject(o.toString()).get("ok"));

        o.setLength(0);
        post(TOKEN, "not json", null, o);
        assertEquals(Boolean.FALSE, Json.parseObject(o.toString()).get("ok"));

        // без готових хуків вводу — зрозуміла помилка, а не зависання
        o.setLength(0);
        post(TOKEN, "{\"cmd\":\"key\",\"key\":\"w\"}", null, o);
        r = Json.parseObject(o.toString());
        assertEquals(Boolean.FALSE, r.get("ok"));
        assertTrue(String.valueOf(r.get("error")).contains("не готові") || String.valueOf(r.get("error")).contains("невідома"));
    }

    /** Повний ланцюжок вводу на підробних callbacks: HTTP → черга → «потік рендеру» (poll) → callback. */
    @Test
    void injectsKeysMouseAndTextThroughQueue() throws Exception {
        final List<String> got = new ArrayList<String>();
        final AtomicReference<Boolean> running = new AtomicReference<Boolean>(Boolean.TRUE);

        org.lwjgl.glfw.GLFWKeyCallbackI key = (w, k, sc, a, m) -> got.add("key:" + k + ":" + a + ":" + m);
        org.lwjgl.glfw.GLFWCharCallbackI chr = (w, cp) -> got.add("char:" + cp);
        org.lwjgl.glfw.GLFWMouseButtonCallbackI mouse = (w, b, a, m) -> got.add("mouse:" + b + ":" + a);
        org.lwjgl.glfw.GLFWCursorPosCallbackI cur = (w, x, y) -> got.add("cursor:" + Math.round(x) + ":" + Math.round(y));
        org.lwjgl.glfw.GLFWScrollCallbackI scroll = (w, x, y) -> got.add("scroll:" + Math.round(y));

        Hooks h = Hooks.I;
        h.accept(new Object[]{"cbKey", 1L, key});
        h.accept(new Object[]{"cbChar", 1L, chr});
        h.accept(new Object[]{"cbMouse", 1L, mouse});
        h.accept(new Object[]{"cbCursor", 1L, cur});
        h.accept(new Object[]{"cbScroll", 1L, scroll});
        h.keepFocus = false; // у тесті немає focus callback

        // «Потік рендеру»: кадри + poll
        Thread render = new Thread(() -> {
            while (running.get()) {
                h.accept(new Object[]{"swap", 1L, org.lwjgl.glfw.GLFW.class});
                h.accept(new Object[]{"poll"});
                try {
                    Thread.sleep(8);
                } catch (InterruptedException e) {
                    return;
                }
            }
        });
        render.setDaemon(true);
        render.start();
        try {
            Thread.sleep(300); // >5 кадрів
            StringBuilder o = new StringBuilder();
            post(TOKEN, "{\"cmd\":\"key\",\"key\":\"w\",\"action\":\"tap\",\"hold_ms\":40,\"mods\":[\"shift\"]}", null, o);
            assertEquals(Boolean.TRUE, Json.parseObject(o.toString()).get("ok"), o.toString());
            synchronized (got) {
                assertTrue(got.contains("key:87:1:1"), got.toString());
                assertTrue(got.contains("key:87:0:1"), got.toString());
            }

            o.setLength(0);
            post(TOKEN, "{\"cmd\":\"type\",\"text\":\"Hi\",\"delay_ms\":5}", null, o);
            assertEquals(Boolean.TRUE, Json.parseObject(o.toString()).get("ok"), o.toString());
            assertTrue(got.contains("char:72") && got.contains("char:105"), got.toString());

            o.setLength(0);
            post(TOKEN, "{\"cmd\":\"mouse_click\",\"button\":\"right\",\"action\":\"tap\",\"hold_ms\":10}", null, o);
            assertEquals(Boolean.TRUE, Json.parseObject(o.toString()).get("ok"), o.toString());
            assertTrue(got.contains("mouse:1:1") && got.contains("mouse:1:0"), got.toString());

            o.setLength(0);
            post(TOKEN, "{\"cmd\":\"mouse_move\",\"x\":200,\"y\":150}", null, o);
            assertEquals(Boolean.TRUE, Json.parseObject(o.toString()).get("ok"), o.toString());
            assertTrue(got.contains("cursor:200:150"), got.toString());

            o.setLength(0);
            post(TOKEN, "{\"cmd\":\"mouse_move\",\"dx\":160,\"dy\":0}", null, o);
            assertEquals(Boolean.TRUE, Json.parseObject(o.toString()).get("ok"), o.toString());
            assertTrue(got.contains("cursor:360:150"), got.toString()); // 200 + 160 двома кроками

            o.setLength(0);
            post(TOKEN, "{\"cmd\":\"scroll\",\"dy\":-3}", null, o);
            assertEquals(Boolean.TRUE, Json.parseObject(o.toString()).get("ok"), o.toString());
            assertTrue(got.contains("scroll:-3"), got.toString());

            o.setLength(0);
            post(TOKEN, "{\"cmd\":\"chat\",\"text\":\"/time set day\"}", null, o);
            assertEquals(Boolean.TRUE, Json.parseObject(o.toString()).get("ok"), o.toString());
            assertTrue(got.contains("key:47:1:0"), "відкриття чату клавішею «/»: " + got);
            assertTrue(got.contains("key:257:1:0"), "Enter: " + got);

            o.setLength(0);
            post(TOKEN, "{\"cmd\":\"key\",\"key\":\"nonexistent_key\"}", null, o);
            assertEquals(Boolean.FALSE, Json.parseObject(o.toString()).get("ok"));

            // FPS: кадри ідуть
            o.setLength(0);
            post(TOKEN, "{\"cmd\":\"status\"}", null, o);
            Map<String, Object> fps = (Map<String, Object>) Json.parseObject(o.toString()).get("fps");
            assertTrue(((Number) fps.get("fps")).intValue() > 5, fps.toString());
            assertFalse(((Number) fps.get("frames_total")).longValue() == 0);
        } finally {
            running.set(Boolean.FALSE);
            render.join(1000);
        }
    }
}
