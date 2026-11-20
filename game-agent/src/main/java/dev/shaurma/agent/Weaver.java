package dev.shaurma.agent;

import org.objectweb.asm.ClassReader;
import org.objectweb.asm.ClassVisitor;
import org.objectweb.asm.ClassWriter;
import org.objectweb.asm.MethodVisitor;
import org.objectweb.asm.Opcodes;
import org.objectweb.asm.Type;

import java.lang.instrument.ClassFileTransformer;
import java.security.ProtectionDomain;

/**
 * Вплітає виклики хука в LWJGL/GLFW та в мережеві класи. Хук знаходиться через
 * {@code System.getProperties().get("shaurma.agent.hook")} і викликається через інтерфейси JDK
 * ({@link java.util.function.Consumer}/{@link java.util.function.Function}), тож модульна ізоляція
 * та різні класлоадери лоадерів (Fabric Knot, Forge, NeoForge) не заважають: усі типи з java.base.
 *
 * <p>Інструкції лише додаються на вході методу (або перед RETURN) без нових переходів, тож
 * StackMapTable не змінюється, і працює COMPUTE_MAXS.
 */
final class Weaver implements ClassFileTransformer {
    static final String HOOK_KEY = "shaurma.agent.hook";
    private static final String CONSUMER = "java/util/function/Consumer";
    private static final String FUNCTION = "java/util/function/Function";

    @Override
    public byte[] transform(ClassLoader loader, String className, Class<?> redefined, ProtectionDomain pd, byte[] classfile) {
        if (className == null) return null;
        try {
            if (className.equals("org/lwjgl/glfw/GLFW")
                    || className.equals("java/net/Socket")
                    || className.equals("sun/nio/ch/SocketChannelImpl")
                    || className.equals("io/netty/channel/unix/Socket")) {
                return weave(className, classfile);
            }
        } catch (Throwable t) {
            System.err.println("[shaurma-agent] не вдалося змінити " + className + ": " + t);
        }
        return null;
    }

    static byte[] weave(final String className, byte[] classfile) {
        ClassReader cr = new ClassReader(classfile);
        ClassWriter cw = new ClassWriter(cr, ClassWriter.COMPUTE_MAXS);
        cr.accept(new ClassVisitor(Opcodes.ASM9, cw) {
            @Override
            public MethodVisitor visitMethod(int access, String name, String desc, String sig, String[] exc) {
                MethodVisitor mv = super.visitMethod(access, name, desc, sig, exc);
                if ((access & Opcodes.ACC_ABSTRACT) != 0 || (access & Opcodes.ACC_NATIVE) != 0) return mv;
                Plan p = plan(className, name, desc);
                return p == null ? mv : new Injector(mv, p, className);
            }
        }, 0);
        return cw.toByteArray();
    }

    // ── План вплетення ───────────────────────────────────────────────────

    private static final class Plan {
        final String tag;
        final boolean atReturn;
        final boolean replaceArg2; // повернути підміну callback у слот 2 (лише focus)

        Plan(String tag, boolean atReturn, boolean replaceArg2) {
            this.tag = tag;
            this.atReturn = atReturn;
            this.replaceArg2 = replaceArg2;
        }
    }

    private static Plan plan(String cls, String name, String desc) {
        if (cls.equals("org/lwjgl/glfw/GLFW")) {
            if (name.equals("glfwSwapBuffers") && desc.equals("(J)V")) return new Plan("swap", false, false);
            if (name.equals("glfwPollEvents") && desc.equals("()V")) return new Plan("poll", true, false);
            if (name.equals("glfwSetInputMode") && desc.equals("(JII)V")) return new Plan("mode", false, false);
            Type[] a = Type.getArgumentTypes(desc);
            if (a.length == 2 && a[0].getSort() == Type.LONG && a[1].getSort() == Type.OBJECT) {
                if (name.equals("glfwSetKeyCallback")) return new Plan("cbKey", false, false);
                if (name.equals("glfwSetCharCallback")) return new Plan("cbChar", false, false);
                if (name.equals("glfwSetCharModsCallback")) return new Plan("cbCharMods", false, false);
                if (name.equals("glfwSetCursorPosCallback")) return new Plan("cbCursor", false, false);
                if (name.equals("glfwSetMouseButtonCallback")) return new Plan("cbMouse", false, false);
                if (name.equals("glfwSetScrollCallback")) return new Plan("cbScroll", false, false);
                if (name.equals("glfwSetWindowFocusCallback")) return new Plan("cbFocus", false, true);
            }
            return null;
        }
        if (cls.equals("java/net/Socket") && name.equals("connect") && desc.equals("(Ljava/net/SocketAddress;I)V")) return new Plan("net", false, false);
        if (cls.equals("sun/nio/ch/SocketChannelImpl") && name.equals("connect") && desc.equals("(Ljava/net/SocketAddress;)Z")) return new Plan("net", false, false);
        if (cls.equals("io/netty/channel/unix/Socket") && name.equals("connect") && desc.equals("(Ljava/net/SocketAddress;)Z")) return new Plan("net", false, false);
        return null;
    }

    // ── Генерація байткоду ───────────────────────────────────────────────

    private static final class Injector extends MethodVisitor {
        private final Plan plan;
        private final String owner;

        Injector(MethodVisitor mv, Plan plan, String owner) {
            super(Opcodes.ASM9, mv);
            this.plan = plan;
            this.owner = owner;
        }

        @Override
        public void visitCode() {
            super.visitCode();
            if (!plan.atReturn) emit();
        }

        @Override
        public void visitInsn(int opcode) {
            if (plan.atReturn && opcode == Opcodes.RETURN) emit();
            super.visitInsn(opcode);
        }

        private void emit() {
            MethodVisitor m = this.mv;
            m.visitMethodInsn(Opcodes.INVOKESTATIC, "java/lang/System", "getProperties", "()Ljava/util/Properties;", false);
            m.visitLdcInsn(HOOK_KEY);
            m.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/util/Hashtable", "get", "(Ljava/lang/Object;)Ljava/lang/Object;", false);
            m.visitTypeInsn(Opcodes.CHECKCAST, plan.replaceArg2 ? FUNCTION : CONSUMER);

            switch (plan.tag) {
                case "swap":
                    // {tag, Long window, Class GLFW}
                    array(m, 3);
                    store(m, 0, plan.tag);
                    loadLong(m, 1, 0);
                    m.visitInsn(Opcodes.DUP);
                    m.visitInsn(Opcodes.ICONST_2);
                    m.visitLdcInsn(Type.getObjectType(owner));
                    m.visitInsn(Opcodes.AASTORE);
                    break;
                case "poll":
                    array(m, 1);
                    store(m, 0, plan.tag);
                    break;
                case "mode":
                    // {tag, Long window, Integer mode, Integer value}
                    array(m, 4);
                    store(m, 0, plan.tag);
                    loadLong(m, 1, 0);
                    loadInt(m, 2, 2);
                    loadInt(m, 3, 3);
                    break;
                case "net":
                    // {tag, SocketAddress} — слот 1 (метод екземпляра)
                    array(m, 2);
                    store(m, 0, plan.tag);
                    m.visitInsn(Opcodes.DUP);
                    m.visitInsn(Opcodes.ICONST_1);
                    m.visitVarInsn(Opcodes.ALOAD, 1);
                    m.visitInsn(Opcodes.AASTORE);
                    break;
                default:
                    // glfwSet*Callback(long window, cb): {tag, Long window, cb}; static → слоти 0-1 (long), 2 (cb)
                    array(m, 3);
                    store(m, 0, plan.tag);
                    loadLong(m, 1, 0);
                    m.visitInsn(Opcodes.DUP);
                    m.visitInsn(Opcodes.ICONST_2);
                    m.visitVarInsn(Opcodes.ALOAD, 2);
                    m.visitInsn(Opcodes.AASTORE);
            }

            if (plan.replaceArg2) {
                m.visitMethodInsn(Opcodes.INVOKEINTERFACE, FUNCTION, "apply", "(Ljava/lang/Object;)Ljava/lang/Object;", true);
                m.visitTypeInsn(Opcodes.CHECKCAST, "org/lwjgl/glfw/GLFWWindowFocusCallbackI");
                m.visitVarInsn(Opcodes.ASTORE, 2);
            } else {
                m.visitMethodInsn(Opcodes.INVOKEINTERFACE, CONSUMER, "accept", "(Ljava/lang/Object;)V", true);
            }
        }

        private static void array(MethodVisitor m, int n) {
            m.visitIntInsn(Opcodes.BIPUSH, n);
            m.visitTypeInsn(Opcodes.ANEWARRAY, "java/lang/Object");
        }

        /** arr[idx] = tag (масив на вершині стеку лишається). */
        private static void store(MethodVisitor m, int idx, String tag) {
            m.visitInsn(Opcodes.DUP);
            m.visitIntInsn(Opcodes.BIPUSH, idx);
            m.visitLdcInsn(tag);
            m.visitInsn(Opcodes.AASTORE);
        }

        /** arr[arrIdx] = Long.valueOf(long у локальному слоті) ; перед викликом на стеку масив. */
        private static void loadLong(MethodVisitor m, int arrIdx, int slot) {
            m.visitInsn(Opcodes.DUP);
            m.visitIntInsn(Opcodes.BIPUSH, arrIdx);
            m.visitVarInsn(Opcodes.LLOAD, slot);
            m.visitMethodInsn(Opcodes.INVOKESTATIC, "java/lang/Long", "valueOf", "(J)Ljava/lang/Long;", false);
            m.visitInsn(Opcodes.AASTORE);
        }

        private static void loadInt(MethodVisitor m, int arrIdx, int slot) {
            m.visitInsn(Opcodes.DUP);
            m.visitIntInsn(Opcodes.BIPUSH, arrIdx);
            m.visitVarInsn(Opcodes.ILOAD, slot);
            m.visitMethodInsn(Opcodes.INVOKESTATIC, "java/lang/Integer", "valueOf", "(I)Ljava/lang/Integer;", false);
            m.visitInsn(Opcodes.AASTORE);
        }
    }
}
