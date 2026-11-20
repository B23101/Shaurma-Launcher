package dev.shaurma.agent;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertSame;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.InputStream;
import java.lang.reflect.InvocationTargetException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;
import java.util.function.Function;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

class WeaverTest {
    /** Child-first завантажувач: вплетені копії заглушок замість тих, що на classpath тесту. */
    static final class Loader extends ClassLoader {
        private final String name;
        private final byte[] bytes;
        private Class<?> defined;

        Loader(String name, byte[] bytes) {
            super(WeaverTest.class.getClassLoader());
            this.name = name;
            this.bytes = bytes;
        }

        @Override
        protected synchronized Class<?> loadClass(String n, boolean resolve) throws ClassNotFoundException {
            if (n.equals(name)) {
                if (defined == null) defined = defineClass(n, bytes, 0, bytes.length);
                return defined;
            }
            return super.loadClass(n, resolve);
        }
    }

    static final class Recorder implements Consumer<Object>, Function<Object, Object> {
        final List<Object[]> events = new ArrayList<Object[]>();

        @Override
        public void accept(Object o) {
            events.add((Object[]) o);
        }

        @Override
        public Object apply(Object o) {
            Object[] a = (Object[]) o;
            events.add(a);
            return new Object[]{"replaced", a[2]}; // не інтерфейс → перевіряємо, що checkcast/store працюють з тим, що повернув хук
        }
    }

    @AfterEach
    void cleanup() {
        System.getProperties().remove(Weaver.HOOK_KEY);
    }

    private static byte[] bytesOf(String internal) throws Exception {
        try (InputStream in = WeaverTest.class.getClassLoader().getResourceAsStream(internal + ".class")) {
            assertNotNull(in, internal);
            java.io.ByteArrayOutputStream b = new java.io.ByteArrayOutputStream();
            byte[] buf = new byte[4096];
            int n;
            while ((n = in.read(buf)) > 0) b.write(buf, 0, n);
            return b.toByteArray();
        }
    }

    private static Class<?> woven(String internal) throws Exception {
        byte[] w = Weaver.weave(internal, bytesOf(internal));
        return new Loader(internal.replace('/', '.'), w).loadClass(internal.replace('/', '.'));
    }

    @Test
    void swapAndPollHooksFire() throws Exception {
        Recorder rec = new Recorder();
        System.getProperties().put(Weaver.HOOK_KEY, rec);
        Class<?> g = woven("org/lwjgl/glfw/GLFW");
        g.getMethod("glfwSwapBuffers", long.class).invoke(null, 42L);
        g.getMethod("glfwPollEvents").invoke(null);
        g.getMethod("glfwSetInputMode", long.class, int.class, int.class).invoke(null, 7L, 0x33001, 0x34003);

        assertEquals("swap", rec.events.get(0)[0]);
        assertEquals(42L, rec.events.get(0)[1]);
        assertSame(g, rec.events.get(0)[2]); // GLFW.class передано хуку
        assertEquals("poll", rec.events.get(1)[0]);
        assertEquals("mode", rec.events.get(2)[0]);
        assertEquals(0x33001, rec.events.get(2)[2]);
        assertEquals(0x34003, rec.events.get(2)[3]);
    }

    @Test
    void callbackCapture() throws Exception {
        Recorder rec = new Recorder();
        System.getProperties().put(Weaver.HOOK_KEY, rec);
        Class<?> g = woven("org/lwjgl/glfw/GLFW");
        org.lwjgl.glfw.GLFWKeyCallbackI key = new org.lwjgl.glfw.GLFWKeyCallbackI() {
            public void invoke(long w, int k, int sc, int a, int m) {
            }
        };
        g.getMethod("glfwSetKeyCallback", long.class, org.lwjgl.glfw.GLFWKeyCallbackI.class).invoke(null, 9L, key);
        org.lwjgl.glfw.GLFWCursorPosCallbackI cur = new org.lwjgl.glfw.GLFWCursorPosCallbackI() {
            public void invoke(long w, double x, double y) {
            }
        };
        g.getMethod("glfwSetCursorPosCallback", long.class, org.lwjgl.glfw.GLFWCursorPosCallbackI.class).invoke(null, 9L, cur);
        assertEquals("cbKey", rec.events.get(0)[0]);
        assertEquals(9L, rec.events.get(0)[1]);
        assertSame(key, rec.events.get(0)[2]);
        assertEquals("cbCursor", rec.events.get(1)[0]);
        assertSame(cur, rec.events.get(1)[2]);
    }

    @Test
    void focusCallbackIsReplacedByHookResult() throws Exception {
        // Хук-Function повертає не callback → checkcast має кинути ClassCastException; перевіряємо, що з нормальним callback все працює
        final org.lwjgl.glfw.GLFWWindowFocusCallbackI replacement = new org.lwjgl.glfw.GLFWWindowFocusCallbackI() {
            public void invoke(long w, boolean f) {
            }
        };
        Consumer<Object> c = new Consumer<Object>() {
            public void accept(Object o) {
            }
        };
        class Both implements Consumer<Object>, Function<Object, Object> {
            public void accept(Object o) {
            }

            public Object apply(Object o) {
                return replacement;
            }
        }
        System.getProperties().put(Weaver.HOOK_KEY, new Both());
        Class<?> g = woven("org/lwjgl/glfw/GLFW");
        org.lwjgl.glfw.GLFWWindowFocusCallbackI original = new org.lwjgl.glfw.GLFWWindowFocusCallbackI() {
            public void invoke(long w, boolean f) {
            }
        };
        Object after = g.getMethod("glfwSetWindowFocusCallback", long.class, org.lwjgl.glfw.GLFWWindowFocusCallbackI.class).invoke(null, 5L, original);
        assertSame(replacement, after); // метод побачив ПІДМІНЕНИЙ callback
        assertTrue(c != null);
    }

    @Test
    void netGuardBlocksPublicAllowsLocal() throws Exception {
        System.getProperties().put(Weaver.HOOK_KEY, Hooks.I);
        Class<?> s = woven("io/netty/channel/unix/Socket");
        Object sock = s.getDeclaredConstructor().newInstance();
        java.lang.reflect.Method connect = s.getMethod("connect", java.net.SocketAddress.class);

        InetSocketAddress pub = new InetSocketAddress(InetAddress.getByAddress(new byte[]{8, 8, 8, 8}), 25565);
        InvocationTargetException ex = assertThrows(InvocationTargetException.class, () -> connect.invoke(sock, pub));
        assertTrue(ex.getCause() instanceof SecurityException);
        assertTrue(ex.getCause().getMessage().contains("8.8.8.8"));

        assertEquals(Boolean.TRUE, connect.invoke(sock, new InetSocketAddress(InetAddress.getByAddress(new byte[]{127, 0, 0, 1}), 25565)));
        assertEquals(Boolean.TRUE, connect.invoke(sock, new InetSocketAddress(InetAddress.getByAddress(new byte[]{(byte) 192, (byte) 168, 1, 20}), 25565)));
        assertEquals(Boolean.TRUE, connect.invoke(sock, new InetSocketAddress(InetAddress.getByAddress(new byte[]{10, 0, 0, 5}), 25565)));
    }

    @Test
    void unrelatedClassesAreUntouched() {
        Weaver w = new Weaver();
        assertEquals(null, w.transform(null, "java/lang/String", null, null, new byte[]{1}));
        assertEquals(null, w.transform(null, null, null, null, new byte[]{1}));
    }
}
