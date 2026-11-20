package org.lwjgl.glfw;

/** Заглушка LWJGL GLFW для тестів вплітання: ті самі імена й дескриптори методів, що й у справжній бібліотеці. */
public class GLFW {
    public static int lastW = 800, lastH = 600;

    public static void glfwSwapBuffers(long window) {
    }

    public static void glfwPollEvents() {
    }

    public static void glfwSetInputMode(long window, int mode, int value) {
    }

    public static void glfwGetFramebufferSize(long window, int[] w, int[] h) {
        w[0] = lastW;
        h[0] = lastH;
    }

    public static void glfwGetWindowSize(long window, int[] w, int[] h) {
        w[0] = lastW;
        h[0] = lastH;
    }

    public static void glfwGetCursorPos(long window, double[] x, double[] y) {
        x[0] = 100;
        y[0] = 100;
    }

    public static Object glfwSetKeyCallback(long window, GLFWKeyCallbackI cb) {
        return null;
    }

    public static Object glfwSetCharCallback(long window, GLFWCharCallbackI cb) {
        return null;
    }

    public static Object glfwSetCharModsCallback(long window, GLFWCharModsCallbackI cb) {
        return null;
    }

    public static Object glfwSetCursorPosCallback(long window, GLFWCursorPosCallbackI cb) {
        return null;
    }

    public static Object glfwSetMouseButtonCallback(long window, GLFWMouseButtonCallbackI cb) {
        return null;
    }

    public static Object glfwSetScrollCallback(long window, GLFWScrollCallbackI cb) {
        return null;
    }

    /** Повертає callback, який опинився в аргументі ПІСЛЯ вплетеного коду (перевірка підміни). */
    public static Object glfwSetWindowFocusCallback(long window, GLFWWindowFocusCallbackI cb) {
        return cb;
    }
}
