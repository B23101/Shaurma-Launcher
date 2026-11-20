package org.lwjgl.glfw;

/** Заглушка інтерфейсу callback LWJGL (для тестів). */
public interface GLFWCharModsCallbackI {
    void invoke(long window, int codepoint, int mods);
}
