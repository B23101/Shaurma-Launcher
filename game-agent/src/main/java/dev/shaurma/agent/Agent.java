package dev.shaurma.agent;

import java.lang.instrument.Instrumentation;
import java.util.HashMap;
import java.util.Map;
import java.util.regex.Pattern;

/**
 * Shaurma Game Agent — точка входу (-javaagent:shaurma-game-agent.jar=port=NNNN;token=XXXX;nick=Claude).
 *
 * <p>Лаунчер підкладає агент ЛИШЕ у запуски, ініційовані ШІ-агентом (окремий акаунт, не гравця).
 * Без коректних port/token агент нічого не вмикає. Мережева заборона (лише локальні адреси) вмикається
 * завжди й вимкнена бути не може.
 */
public final class Agent {
    public static final String VERSION = "1.0.0";
    private static final Pattern TOKEN = Pattern.compile("[A-Za-z0-9_-]{16,128}");
    private static final Pattern NICK = Pattern.compile("[A-Za-z0-9_]{3,16}");

    private Agent() {
    }

    static Map<String, String> parseArgs(String args) {
        Map<String, String> m = new HashMap<String, String>();
        if (args == null) return m;
        for (String part : args.split(";")) {
            int e = part.indexOf('=');
            if (e > 0) m.put(part.substring(0, e).trim(), part.substring(e + 1).trim());
        }
        return m;
    }

    public static void premain(String args, Instrumentation inst) {
        try {
            Map<String, String> a = parseArgs(args);
            int port;
            try {
                port = Integer.parseInt(a.getOrDefault("port", ""));
            } catch (NumberFormatException e) {
                port = -1;
            }
            String token = a.getOrDefault("token", "");
            String nick = a.getOrDefault("nick", "Claude");
            if (port < 1024 || port > 65535 || !TOKEN.matcher(token).matches() || !NICK.matcher(nick).matches()) {
                System.err.println("[shaurma-agent] вимкнено: потрібні коректні port, token і nick");
                return;
            }
            // 1) Хук у системних властивостях — єдиний канал для вплетеного байткоду.
            System.getProperties().put(Weaver.HOOK_KEY, Hooks.I);
            // 2) Вплітання: GLFW (на етапі завантаження) і мережа (також для вже завантажених класів JDK).
            Weaver w = new Weaver();
            inst.addTransformer(w, true);
            retransform(inst, "java.net.Socket");
            retransform(inst, "sun.nio.ch.SocketChannelImpl");
            // 3) Керуючий сервер (лише 127.0.0.1 + токен).
            new Control(port, token, nick).start();
            System.err.println("[shaurma-agent] " + VERSION + " активний (127.0.0.1:" + port + ", нік " + nick + "); мережу обмежено локальними адресами");
        } catch (Throwable t) {
            System.err.println("[shaurma-agent] не запустився: " + t);
        }
    }

    private static void retransform(Instrumentation inst, String name) {
        try {
            Class<?> c = Class.forName(name, false, null);
            if (inst.isModifiableClass(c)) inst.retransformClasses(c);
        } catch (Throwable t) {
            System.err.println("[shaurma-agent] retransform " + name + ": " + t);
        }
    }
}
