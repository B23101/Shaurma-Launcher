package dev.shaurma.agent;

import java.util.HashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

/** Імена клавіш → коди GLFW (однакові у всіх версіях Minecraft з LWJGL 3). */
final class Keys {
    static final int MOD_SHIFT = 1, MOD_CONTROL = 2, MOD_ALT = 4, MOD_SUPER = 8;
    private static final Map<String, Integer> MAP = new HashMap<String, Integer>();

    static {
        for (char c = 'a'; c <= 'z'; c++) MAP.put(String.valueOf(c), (int) Character.toUpperCase(c));
        for (char c = '0'; c <= '9'; c++) MAP.put(String.valueOf(c), (int) c);
        for (int f = 1; f <= 25; f++) MAP.put("f" + f, 289 + f);
        for (int n = 0; n <= 9; n++) MAP.put("kp" + n, 320 + n);
        put(32, "space");
        put(39, "apostrophe", "quote");
        put(44, "comma");
        put(45, "minus");
        put(46, "period", "dot");
        put(47, "slash");
        put(59, "semicolon");
        put(61, "equal", "equals");
        put(91, "left_bracket");
        put(92, "backslash");
        put(93, "right_bracket");
        put(96, "grave", "backtick");
        put(256, "escape", "esc");
        put(257, "enter", "return");
        put(258, "tab");
        put(259, "backspace");
        put(260, "insert");
        put(261, "delete", "del");
        put(262, "right");
        put(263, "left");
        put(264, "down");
        put(265, "up");
        put(266, "page_up", "pageup");
        put(267, "page_down", "pagedown");
        put(268, "home");
        put(269, "end");
        put(280, "caps_lock");
        put(340, "shift", "left_shift", "lshift");
        put(341, "ctrl", "control", "left_control", "lctrl");
        put(342, "alt", "left_alt", "lalt");
        put(344, "right_shift", "rshift");
        put(345, "right_control", "rctrl");
        put(346, "right_alt", "ralt");
    }

    private Keys() {
    }

    private static void put(int code, String... names) {
        for (String n : names) MAP.put(n, code);
    }

    /** Код GLFW для імені клавіші; -1, якщо невідоме. */
    static int code(String name) {
        if (name == null) return -1;
        Integer c = MAP.get(name.trim().toLowerCase(Locale.ROOT).replace('-', '_').replace(' ', '_'));
        return c == null ? -1 : c;
    }

    /** Бітова маска модифікаторів із списку імен. */
    static int mods(List<?> names) {
        int m = 0;
        if (names == null) return 0;
        for (Object o : names) {
            String n = String.valueOf(o).toLowerCase(Locale.ROOT);
            if (n.equals("shift")) m |= MOD_SHIFT;
            else if (n.equals("ctrl") || n.equals("control")) m |= MOD_CONTROL;
            else if (n.equals("alt")) m |= MOD_ALT;
            else if (n.equals("super") || n.equals("win")) m |= MOD_SUPER;
        }
        return m;
    }

    /** Кнопка миші GLFW: 0 left, 1 right, 2 middle, 3..7 додаткові; -1 невідома. */
    static int mouseButton(String name) {
        if (name == null) return 0;
        switch (name.trim().toLowerCase(Locale.ROOT)) {
            case "left": case "lmb": case "": return 0;
            case "right": case "rmb": return 1;
            case "middle": case "mmb": return 2;
            case "back": return 3;
            case "forward": return 4;
            default: return -1;
        }
    }
}
