package dev.shaurma.agent;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.net.InetAddress;
import java.util.Arrays;
import java.util.List;
import java.util.Map;

import org.junit.jupiter.api.Test;

class CoreTest {
    // ── Json ──
    @Test
    void jsonRoundTrip() {
        Map<String, Object> m = Json.parseObject("{\"cmd\":\"key\",\"n\":12,\"f\":1.5,\"b\":true,\"z\":null,\"l\":[1,\"a\\n\",{\"x\":2}],\"u\":\"\\u0414\"}");
        assertEquals("key", m.get("cmd"));
        assertEquals(12L, m.get("n"));
        assertEquals(1.5, m.get("f"));
        assertEquals(Boolean.TRUE, m.get("b"));
        assertEquals("Д", m.get("u"));
        String out = Json.write(m);
        assertEquals(m, Json.parseObject(out));
    }

    @Test
    void jsonRejectsGarbage() {
        for (String bad : new String[]{"", "{", "{\"a\":}", "[1,]", "{\"a\":1} x", "nul", "\"abc"}) {
            assertThrows(IllegalArgumentException.class, () -> Json.parse(bad), bad);
        }
        StringBuilder deep = new StringBuilder();
        for (int i = 0; i < 100; i++) deep.append('[');
        assertThrows(IllegalArgumentException.class, () -> Json.parse(deep.toString()));
    }

    @Test
    void jsonEscapesOutput() {
        assertEquals("\"a\\\"b\\\\c\\n\\u0001\"", Json.write("a\"b\\c\n\u0001"));
    }

    // ── Net ──
    private static InetAddress ip(String s) throws Exception {
        return InetAddress.getByName(s);
    }

    @Test
    void networkGuardClassifiesAddresses() throws Exception {
        for (String ok : new String[]{"127.0.0.1", "127.5.5.5", "::1", "10.1.2.3", "172.16.0.9", "172.31.255.1", "192.168.0.10", "169.254.1.1", "fe80::1", "fd12:3456::1", "fc00::1"}) {
            assertTrue(Net.isAllowed(ip(ok)), ok);
        }
        for (String bad : new String[]{"8.8.8.8", "1.1.1.1", "172.32.0.1", "172.15.0.1", "193.168.0.1", "2001:4860:4860::8888", "100.64.0.1", "::ffff:8.8.8.8"}) {
            assertFalse(Net.isAllowed(ip(bad)), bad);
        }
        assertFalse(Net.isAllowed(null));
    }

    // ── Keys ──
    @Test
    void keyNames() {
        assertEquals(87, Keys.code("w"));
        assertEquals(87, Keys.code("W"));
        assertEquals(32, Keys.code("space"));
        assertEquals(256, Keys.code("Escape"));
        assertEquals(292, Keys.code("f3"));
        assertEquals(340, Keys.code("left shift"));
        assertEquals(-1, Keys.code("nope"));
        assertEquals(-1, Keys.code(null));
        assertEquals(Keys.MOD_SHIFT | Keys.MOD_CONTROL, Keys.mods(Arrays.asList("shift", "ctrl")));
        assertEquals(0, Keys.mods((List<?>) null));
        assertEquals(0, Keys.mouseButton("left"));
        assertEquals(1, Keys.mouseButton("right"));
        assertEquals(2, Keys.mouseButton("middle"));
        assertEquals(-1, Keys.mouseButton("foo"));
    }

    // ── Agent ──
    @Test
    void agentArgsParsing() {
        Map<String, String> a = Agent.parseArgs("port=47001;token=abc_DEF-123;nick=Claude");
        assertEquals("47001", a.get("port"));
        assertEquals("abc_DEF-123", a.get("token"));
        assertEquals("Claude", a.get("nick"));
        assertTrue(Agent.parseArgs(null).isEmpty());
        assertTrue(Agent.parseArgs("garbage").isEmpty());
    }
}
