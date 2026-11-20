package io.netty.channel.unix;

import java.net.SocketAddress;

/** Заглушка netty-сокета: метод connect з тим самим дескриптором, що й у справжнього. */
public class Socket {
    public boolean connect(SocketAddress addr) {
        return true;
    }
}
