package org.lwjgl.glfw;

/** Заглушка інтерфейсу callback LWJGL (для тестів). */
public interface GLFWCharCallbackI {
    void invoke(long window, int codepoint);
}
