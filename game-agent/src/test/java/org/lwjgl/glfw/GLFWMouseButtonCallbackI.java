package org.lwjgl.glfw;

/** Заглушка інтерфейсу callback LWJGL (для тестів). */
public interface GLFWMouseButtonCallbackI {
    void invoke(long window, int button, int action, int mods);
}
