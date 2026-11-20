package dev.shaurma.agent;

import java.awt.image.BufferedImage;
import java.io.ByteArrayOutputStream;
import java.lang.reflect.Method;
import java.nio.ByteBuffer;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

import javax.imageio.IIOImage;
import javax.imageio.ImageIO;
import javax.imageio.ImageWriteParam;
import javax.imageio.ImageWriter;
import javax.imageio.stream.MemoryCacheImageOutputStream;

/**
 * Окремий скріншот гри: кадр читається з заднього буфера прямо перед показом (glReadPixels) і НЕ потрапляє
 * в папку screenshots. Фокус вікна не потрібен. Працює однаково в усіх версіях (LWJGL 3).
 */
final class Capture {
    private Capture() {
    }

    /** Запит на кадр: ставиться з потоку HTTP, виконується в хуку swap на потоці рендеру. */
    static final class Request {
        private final CountDownLatch done = new CountDownLatch(1);
        volatile int w, h;
        volatile byte[] rgb; // знизу вгору, 3 байти на піксель
        volatile String error;

        void fulfil(long window, Class<?> glfw) {
            try {
                read(window, glfw, this);
            } catch (Throwable t) {
                Throwable c = t.getCause() != null ? t.getCause() : t;
                error = "читання кадру: " + c;
            } finally {
                done.countDown();
            }
        }

        boolean await(long ms) throws InterruptedException {
            return done.await(ms, TimeUnit.MILLISECONDS);
        }
    }

    private static void read(long window, Class<?> glfw, Request out) throws Exception {
        ClassLoader cl = glfw.getClassLoader();
        Class<?> gl11 = Class.forName("org.lwjgl.opengl.GL11", true, cl);
        Class<?> gl30 = Class.forName("org.lwjgl.opengl.GL30", true, cl);
        Class<?> bu = Class.forName("org.lwjgl.BufferUtils", true, cl);

        int[] w = new int[1], h = new int[1];
        glfw.getMethod("glfwGetFramebufferSize", long.class, int[].class, int[].class).invoke(null, window, w, h);
        if (w[0] <= 0 || h[0] <= 0) throw new IllegalStateException("порожній буфер кадру");

        Method getInt = gl11.getMethod("glGetInteger", int.class);
        Method readBuffer = gl11.getMethod("glReadBuffer", int.class);
        Method pixelStore = gl11.getMethod("glPixelStorei", int.class, int.class);
        Method readPixels = gl11.getMethod("glReadPixels", int.class, int.class, int.class, int.class, int.class, int.class, ByteBuffer.class);
        Method bind = gl30.getMethod("glBindFramebuffer", int.class, int.class);

        final int GL_READ_FRAMEBUFFER = 0x8CA8, GL_READ_FRAMEBUFFER_BINDING = 0x8CAA;
        final int GL_READ_BUFFER = 0x0C02, GL_BACK = 0x0405, GL_PACK_ALIGNMENT = 0x0D05;
        final int GL_RGB = 0x1907, GL_UNSIGNED_BYTE = 0x1401;

        int prevFbo = (Integer) getInt.invoke(null, GL_READ_FRAMEBUFFER_BINDING);
        int prevBuf = (Integer) getInt.invoke(null, GL_READ_BUFFER);
        int prevAlign = (Integer) getInt.invoke(null, GL_PACK_ALIGNMENT);
        try {
            bind.invoke(null, GL_READ_FRAMEBUFFER, 0); // читаємо з вікна, а не з FBO гри
            readBuffer.invoke(null, GL_BACK);
            pixelStore.invoke(null, GL_PACK_ALIGNMENT, 1);
            ByteBuffer buf = (ByteBuffer) bu.getMethod("createByteBuffer", int.class).invoke(null, w[0] * h[0] * 3);
            readPixels.invoke(null, 0, 0, w[0], h[0], GL_RGB, GL_UNSIGNED_BYTE, buf);
            byte[] bytes = new byte[w[0] * h[0] * 3];
            buf.rewind();
            buf.get(bytes);
            out.w = w[0];
            out.h = h[0];
            out.rgb = bytes;
        } finally {
            pixelStore.invoke(null, GL_PACK_ALIGNMENT, prevAlign);
            readBuffer.invoke(null, prevBuf);
            bind.invoke(null, GL_READ_FRAMEBUFFER, prevFbo);
        }
    }

    /** RGB знизу вгору → зображення (перевернуте, за потреби зменшене) → PNG/JPEG. Виконується поза потоком рендеру. */
    static byte[] encode(int w, int h, byte[] rgb, int maxWidth, String format) throws Exception {
        BufferedImage img = new BufferedImage(w, h, BufferedImage.TYPE_INT_RGB);
        int[] row = new int[w];
        for (int y = 0; y < h; y++) {
            int src = (h - 1 - y) * w * 3;
            for (int x = 0; x < w; x++) {
                int p = src + x * 3;
                row[x] = ((rgb[p] & 0xff) << 16) | ((rgb[p + 1] & 0xff) << 8) | (rgb[p + 2] & 0xff);
            }
            img.setRGB(0, y, w, 1, row, 0, w);
        }
        if (maxWidth > 0 && w > maxWidth) {
            int nw = maxWidth, nh = Math.max(1, (int) Math.round(h * (maxWidth / (double) w)));
            BufferedImage small = new BufferedImage(nw, nh, BufferedImage.TYPE_INT_RGB);
            java.awt.Graphics2D g = small.createGraphics();
            g.setRenderingHint(java.awt.RenderingHints.KEY_INTERPOLATION, java.awt.RenderingHints.VALUE_INTERPOLATION_BILINEAR);
            g.drawImage(img, 0, 0, nw, nh, null);
            g.dispose();
            img = small;
        }
        ByteArrayOutputStream bos = new ByteArrayOutputStream();
        if ("jpeg".equals(format) || "jpg".equals(format)) {
            ImageWriter wr = ImageIO.getImageWritersByFormatName("jpeg").next();
            ImageWriteParam p = wr.getDefaultWriteParam();
            p.setCompressionMode(ImageWriteParam.MODE_EXPLICIT);
            p.setCompressionQuality(0.8f);
            MemoryCacheImageOutputStream ios = new MemoryCacheImageOutputStream(bos);
            wr.setOutput(ios);
            wr.write(null, new IIOImage(img, null, null), p);
            ios.close();
            wr.dispose();
        } else {
            ImageIO.write(img, "png", bos);
        }
        return bos.toByteArray();
    }
}
