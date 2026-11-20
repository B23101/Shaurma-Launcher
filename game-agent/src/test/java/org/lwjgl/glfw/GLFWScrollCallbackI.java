package org.lwjgl.glfw;

/** Заглушка інтерфейсу callback LWJGL (для тестів). */
public interface GLFWScrollCallbackI {
    void invoke(long window, double xoffset, double yoffset);
}
