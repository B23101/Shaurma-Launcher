package dev.shaurma.agent;

import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.List;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;
import java.util.function.Function;

/**
 * Центральний стан агента. Об'єкт лежить у системних властивостях JVM; байткод, вплетений у GLFW і
 * мережеві класи, викликає його через {@link Consumer}/{@link Function}.
 *
 * <p>Усі виклики GLFW/OpenGL робляться ЛИШЕ з потоку рендеру (з хуків swap/poll) — так само, як у гри.
 * Вплетені виклики ніколи не кидають винятків (крім навмисного блокування мережі).
 */
public final class Hooks implements Consumer<Object>, Function<Object, Object> {
    static final Hooks I = new Hooks();

    // ── захоплені GLFW-callbacks ──
    volatile long window;
    volatile Object cbKey, cbChar, cbCharMods, cbCursor, cbMouse, cbScroll, cbFocus;
    volatile Class<?> glfw;
    volatile boolean cursorDisabled;
    volatile boolean keepFocus = true;
    volatile boolean focusSent;
    volatile int fbW, fbH, winW, winH;

    // ── кадри ──
    private static final int RING = 4096;
    private final long[] ring = new long[RING];
    private long frames; // лише потік рендеру
    private volatile long framesPublished;
    private volatile long lastFrameNanos;
    volatile long startedNanos = System.nanoTime();

    // ── черга задач потоку рендеру ──
    private static final class Task {
        final long due;
        final Runnable run;

        Task(long due, Runnable run) {
            this.due = due;
            this.run = run;
        }
    }

    private final ConcurrentLinkedQueue<Task> inbox = new ConcurrentLinkedQueue<Task>();
    private final List<Task> timed = new ArrayList<Task>(); // лише потік рендеру
    volatile Capture.Request pendingShot;
    volatile String lastError = "";

    // ── віртуальний курсор ──
    private double vx, vy;
    private boolean vInit;

    @Override
    public void accept(Object o) {
        Object[] a = (Object[]) o;
        String tag = (String) a[0];
        try {
            switch (tag) {
                case "net":
                    Net.check((java.net.SocketAddress) a[1]);
                    return;
                case "swap":
                    onSwap(((Long) a[1]).longValue(), (Class<?>) a[2]);
                    return;
                case "poll":
                    onPoll();
                    return;
                case "mode":
                    if (((Integer) a[2]).intValue() == 0x33001) cursorDisabled = ((Integer) a[3]).intValue() == 0x34003;
                    return;
                case "cbKey": window = ((Long) a[1]).longValue(); cbKey = a[2]; return;
                case "cbChar": window = ((Long) a[1]).longValue(); cbChar = a[2]; return;
                case "cbCharMods": window = ((Long) a[1]).longValue(); cbCharMods = a[2]; return;
                case "cbCursor": window = ((Long) a[1]).longValue(); cbCursor = a[2]; return;
                case "cbMouse": window = ((Long) a[1]).longValue(); cbMouse = a[2]; return;
                case "cbScroll": window = ((Long) a[1]).longValue(); cbScroll = a[2]; return;
                default:
            }
        } catch (SecurityException e) {
            throw e; // навмисне блокування мережі
        } catch (Throwable t) {
            lastError = tag + ": " + t;
        }
    }

    /** Для glfwSetWindowFocusCallback: повертає callback-обгортку, що глушить втрату фокусу. */
    @Override
    public Object apply(Object o) {
        Object[] a = (Object[]) o;
        Object cb = a[2];
        try {
            window = ((Long) a[1]).longValue();
            cbFocus = cb;
            if (cb == null) return null;
            final Object orig = cb;
            Class<?> iface = findInterface(cb.getClass(), "GLFWWindowFocusCallbackI");
            if (iface == null) return cb;
            return Proxy.newProxyInstance(iface.getClassLoader(), new Class<?>[]{iface}, new InvocationHandler() {
                @Override
                public Object invoke(Object proxy, Method m, Object[] args) throws Throwable {
                    if (m.getDeclaringClass() == Object.class) return m.invoke(orig, args);
                    // invoke(long window, boolean focused): поки агент тримає фокус, «втрату» ігноруємо
                    if (keepFocus && args != null && args.length == 2 && Boolean.FALSE.equals(args[1])) return null;
                    return m.invoke(orig, args);
                }
            });
        } catch (Throwable t) {
            lastError = "focus: " + t;
            return cb;
        }
    }

    // ── потік рендеру ───────────────────────────────────────────────────

    private void onSwap(long win, Class<?> glfwClass) {
        long now = System.nanoTime();
        if (glfw == null) glfw = glfwClass;
        if (window == 0) window = win;
        ring[(int) (frames % RING)] = now;
        frames++;
        framesPublished = frames;
        lastFrameNanos = now;

        // Розміри вікна/буфера кадру — не частіше разу на 20 кадрів.
        if (frames % 20 == 1) {
            int[] w = new int[1], h = new int[1];
            try {
                glfwClass.getMethod("glfwGetFramebufferSize", long.class, int[].class, int[].class).invoke(null, win, w, h);
                fbW = w[0];
                fbH = h[0];
                glfwClass.getMethod("glfwGetWindowSize", long.class, int[].class, int[].class).invoke(null, win, w, h);
                winW = w[0];
                winH = h[0];
            } catch (Throwable t) {
                lastError = "size: " + t;
            }
        }

        Capture.Request req = pendingShot;
        if (req != null) {
            pendingShot = null;
            req.fulfil(win, glfwClass);
        }
    }

    private void onPoll() {
        Task t;
        while ((t = inbox.poll()) != null) timed.add(t);
        if (timed.isEmpty()) return;
        long now = System.nanoTime();
        for (Iterator<Task> it = timed.iterator(); it.hasNext(); ) {
            Task x = it.next();
            if (x.due <= now) {
                it.remove();
                try {
                    x.run.run();
                } catch (Throwable e) {
                    lastError = "task: " + e;
                }
            }
        }
        if (keepFocus && !focusSent && cbFocus != null && window != 0 && framesPublished > 30) {
            focusSent = true;
            invokeCallback(cbFocus, window, Boolean.TRUE);
        }
    }

    // ── API для керування (потоки HTTP) ─────────────────────────────────

    /** Чи готові хуки вводу (гра дійшла до налаштування callbacks і малює кадри). */
    boolean ready() {
        return window != 0 && cbKey != null && cbMouse != null && framesPublished > 5
                && System.nanoTime() - lastFrameNanos < TimeUnit.SECONDS.toNanos(5);
    }

    void schedule(long delayMs, Runnable r) {
        inbox.add(new Task(System.nanoTime() + delayMs * 1_000_000L, r));
    }

    /** Виконує набір кроків (кожен — із власною затримкою) і чекає завершення на потоці рендеру. */
    boolean runSteps(List<long[]> delays, List<Runnable> steps, long extraWaitMs) throws InterruptedException {
        if (steps.size() != delays.size()) throw new IllegalArgumentException("steps/delays");
        final CountDownLatch done = new CountDownLatch(steps.size());
        long total = 0;
        for (int i = 0; i < steps.size(); i++) {
            final Runnable r = steps.get(i);
            final long d = delays.get(i)[0];
            total = Math.max(total, d);
            schedule(d, new Runnable() {
                @Override
                public void run() {
                    try {
                        r.run();
                    } finally {
                        done.countDown();
                    }
                }
            });
        }
        return done.await(total + extraWaitMs, TimeUnit.MILLISECONDS);
    }

    // ── введення (викликається з потоку рендру через schedule) ──────────

    void key(int key, int action, int mods) {
        invokeCallback(cbKey, window, Integer.valueOf(key), Integer.valueOf(0), Integer.valueOf(action), Integer.valueOf(mods));
    }

    void chr(int codepoint) {
        if (cbCharMods != null && cbChar == null) invokeCallback(cbCharMods, window, Integer.valueOf(codepoint), Integer.valueOf(0));
        else if (cbChar != null) invokeCallback(cbChar, window, Integer.valueOf(codepoint));
        else if (cbCharMods != null) invokeCallback(cbCharMods, window, Integer.valueOf(codepoint), Integer.valueOf(0));
    }

    void mouseButton(int button, int action, int mods) {
        invokeCallback(cbMouse, window, Integer.valueOf(button), Integer.valueOf(action), Integer.valueOf(mods));
    }

    void scroll(double dx, double dy) {
        invokeCallback(cbScroll, window, Double.valueOf(dx), Double.valueOf(dy));
    }

    /** Курсор: relative=true — зсув, інакше абсолютні координати у вікні. */
    void cursor(double x, double y, boolean relative) {
        if (!vInit) {
            try {
                double[] ax = new double[1], ay = new double[1];
                glfw.getMethod("glfwGetCursorPos", long.class, double[].class, double[].class).invoke(null, window, ax, ay);
                vx = ax[0];
                vy = ay[0];
            } catch (Throwable t) {
                vx = winW / 2.0;
                vy = winH / 2.0;
            }
            vInit = true;
        }
        if (relative) {
            vx += x;
            vy += y;
        } else {
            vx = x;
            vy = y;
        }
        if (!cursorDisabled) { // у GUI тримаємо курсор у межах вікна
            vx = Math.max(0, Math.min(winW > 0 ? winW : vx, vx));
            vy = Math.max(0, Math.min(winH > 0 ? winH : vy, vy));
        }
        invokeCallback(cbCursor, window, Double.valueOf(vx), Double.valueOf(vy));
    }

    // ── статистика кадрів ───────────────────────────────────────────────

    java.util.Map<String, Object> fpsStats() {
        java.util.Map<String, Object> m = new java.util.LinkedHashMap<String, Object>();
        long n = framesPublished;
        m.put("frames_total", n);
        if (n < 3) {
            m.put("fps", 0);
            return m;
        }
        long now = System.nanoTime();
        int count = (int) Math.min(n, RING);
        // кадри за останню секунду / 5 секунд
        int in1 = 0, in5 = 0;
        for (int k = 0; k < count; k++) {
            long ts = ring[(int) ((n - 1 - k) % RING)];
            long age = now - ts;
            if (age <= 1_000_000_000L) in1++;
            if (age <= 5_000_000_000L) in5++;
            else break;
        }
        m.put("fps", in1);
        m.put("fps_avg_5s", Math.round(in5 / 5.0 * 10) / 10.0);
        // тривалості кадрів за останні ~600 кадрів
        int take = (int) Math.min(count - 1, 600);
        double[] ms = new double[take];
        for (int k = 0; k < take; k++) {
            long a = ring[(int) ((n - 1 - k) % RING)];
            long b = ring[(int) ((n - 2 - k) % RING)];
            ms[k] = (a - b) / 1e6;
        }
        if (take > 0) {
            double sum = 0, max = 0;
            for (double v : ms) {
                sum += v;
                max = Math.max(max, v);
            }
            java.util.Arrays.sort(ms);
            int idx99 = Math.min(take - 1, (int) Math.floor(take * 0.99));
            m.put("frame_ms_avg", Math.round(sum / take * 100) / 100.0);
            m.put("frame_ms_p99", Math.round(ms[idx99] * 100) / 100.0);
            m.put("frame_ms_max", Math.round(max * 100) / 100.0);
            double p99 = ms[idx99];
            m.put("fps_1pct_low", p99 > 0 ? Math.round(1000.0 / p99 * 10) / 10.0 : 0);
        }
        return m;
    }

    // ── утиліти ─────────────────────────────────────────────────────────

    static Class<?> findInterface(Class<?> c, String simpleName) {
        for (Class<?> k = c; k != null; k = k.getSuperclass()) {
            for (Class<?> i : k.getInterfaces()) {
                Class<?> r = findIn(i, simpleName);
                if (r != null) return r;
            }
        }
        return null;
    }

    private static Class<?> findIn(Class<?> i, String simple) {
        if (i.getSimpleName().equals(simple)) return i;
        for (Class<?> s : i.getInterfaces()) {
            Class<?> r = findIn(s, simple);
            if (r != null) return r;
        }
        return null;
    }

    /** Викликає метод invoke(...) інтерфейсу LWJGL-callback; перший аргумент — handle вікна. */
    void invokeCallback(Object cb, long win, Object... rest) {
        if (cb == null) return;
        try {
            Method m = null;
            for (Class<?> k = cb.getClass(); k != null && m == null; k = k.getSuperclass()) {
                for (Class<?> i : k.getInterfaces()) {
                    m = invokeOf(i, rest.length + 1);
                    if (m != null) break;
                }
            }
            if (m == null) {
                lastError = "callback без публічного invoke: " + cb.getClass().getName();
                return;
            }
            Object[] args = new Object[rest.length + 1];
            args[0] = Long.valueOf(win);
            System.arraycopy(rest, 0, args, 1, rest.length);
            m.invoke(cb, args);
        } catch (Throwable t) {
            Throwable c = t.getCause() != null ? t.getCause() : t;
            lastError = "callback: " + c;
        }
    }

    private static Method invokeOf(Class<?> i, int nargs) {
        if (i.getName().startsWith("org.lwjgl.glfw.GLFW") && i.getName().endsWith("CallbackI")) {
            for (Method m : i.getMethods()) {
                if (m.getName().equals("invoke") && m.getParameterTypes().length == nargs) return m;
            }
        }
        for (Class<?> s : i.getInterfaces()) {
            Method m = invokeOf(s, nargs);
            if (m != null) return m;
        }
        return null;
    }
}
