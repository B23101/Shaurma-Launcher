package dev.shaurma.agent;

import java.net.Inet6Address;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Мережева заборона: поки агент завантажений, гра може підключатися ЛИШЕ до локальних адрес
 * (loopback, приватні мережі, link-local, IPv6 ULA). Це захист від зловживання агентом
 * (боти, атаки на публічні сервери). Вимкнути заборону не можна.
 */
final class Net {
    static final AtomicLong BLOCKED = new AtomicLong();
    static volatile String lastBlocked = "";

    private Net() {
    }

    /** true, якщо до адреси дозволено підключатися. */
    static boolean isAllowed(InetAddress a) {
        if (a == null) return false;
        if (a.isLoopbackAddress() || a.isAnyLocalAddress() || a.isSiteLocalAddress() || a.isLinkLocalAddress()) return true;
        if (a instanceof Inet6Address) {
            byte[] b = a.getAddress();
            return (b[0] & 0xfe) == 0xfc; // fc00::/7 — унікальні локальні адреси
        }
        return false;
    }

    /** Перевірка адреси сокета; кидає SecurityException для нелокальної (виклик іде з JDK/netty-коду). */
    static void check(SocketAddress sa) {
        if (!(sa instanceof InetSocketAddress)) return;
        InetSocketAddress isa = (InetSocketAddress) sa;
        if (isa.isUnresolved()) return; // JDK/Netty самі завершать помилкою до з'єднання
        InetAddress ia = isa.getAddress();
        if (!isAllowed(ia)) {
            BLOCKED.incrementAndGet();
            lastBlocked = ia == null ? "?" : ia.getHostAddress();
            throw new SecurityException("shaurma-agent: з'єднання з нелокальною адресою заблоковано: " + lastBlocked);
        }
    }
}
