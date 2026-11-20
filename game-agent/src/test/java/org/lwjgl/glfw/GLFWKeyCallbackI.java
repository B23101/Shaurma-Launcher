package org.lwjgl.glfw;

/** Заглушка інтерфейсу callback LWJGL (для тестів). */
public interface GLFWKeyCallbackI {
    void invoke(long window, int key, int scancode, int action, int mods);
}
