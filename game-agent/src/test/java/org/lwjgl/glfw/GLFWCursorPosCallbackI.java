package org.lwjgl.glfw;

/** Заглушка інтерфейсу callback LWJGL (для тестів). */
public interface GLFWCursorPosCallbackI {
    void invoke(long window, double xpos, double ypos);
}
