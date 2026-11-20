package org.lwjgl.glfw;

/** Заглушка інтерфейсу callback LWJGL (для тестів). */
public interface GLFWWindowFocusCallbackI {
    void invoke(long window, boolean focused);
}
