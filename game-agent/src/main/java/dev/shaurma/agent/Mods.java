package dev.shaurma.agent;

import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** Список модів через API лоадера (Fabric/Quilt, Forge, NeoForge) — за допомогою рефлексії, best-effort. */
final class Mods {
    private Mods() {
    }

    static Map<String, Object> list(Object sampleGameObject, Class<?> glfw) {
        List<ClassLoader> loaders = new ArrayList<ClassLoader>();
        if (sampleGameObject != null && sampleGameObject.getClass().getClassLoader() != null) loaders.add(sampleGameObject.getClass().getClassLoader());
        if (glfw != null && glfw.getClassLoader() != null) loaders.add(glfw.getClassLoader());
        ClassLoader ctx = Thread.currentThread().getContextClassLoader();
        if (ctx != null) loaders.add(ctx);
        loaders.add(ClassLoader.getSystemClassLoader());

        for (ClassLoader cl : loaders) {
            Map<String, Object> r = fabric(cl);
            if (r == null) r = forge(cl, "net.minecraftforge.fml.ModList", "forge");
            if (r == null) r = forge(cl, "net.neoforged.fml.ModList", "neoforge");
            if (r != null) return r;
        }
        Map<String, Object> m = new LinkedHashMap<String, Object>();
        m.put("loader", "unknown");
        m.put("count", 0);
        return m;
    }

    private static Map<String, Object> fabric(ClassLoader cl) {
        try {
            Class<?> fl = Class.forName("net.fabricmc.loader.api.FabricLoader", false, cl);
            Object inst = fl.getMethod("getInstance").invoke(null);
            Collection<?> mods = (Collection<?>) fl.getMethod("getAllMods").invoke(inst);
            Class<?> mc = Class.forName("net.fabricmc.loader.api.ModContainer", false, cl);
            Class<?> md = Class.forName("net.fabricmc.loader.api.metadata.ModMetadata", false, cl);
            Class<?> ver = Class.forName("net.fabricmc.loader.api.Version", false, cl);
            List<String> out = new ArrayList<String>();
            for (Object m : mods) {
                Object meta = mc.getMethod("getMetadata").invoke(m);
                String id = String.valueOf(md.getMethod("getId").invoke(meta));
                Object v = md.getMethod("getVersion").invoke(meta);
                out.add(id + " " + ver.getMethod("getFriendlyString").invoke(v));
                if (out.size() >= 600) break;
            }
            return result("fabric-or-quilt", out);
        } catch (Throwable t) {
            return null;
        }
    }

    private static Map<String, Object> forge(ClassLoader cl, String modListClass, String name) {
        try {
            Class<?> ml = Class.forName(modListClass, false, cl);
            Object inst = ml.getMethod("get").invoke(null);
            Collection<?> mods = (Collection<?>) ml.getMethod("getMods").invoke(inst);
            List<String> out = new ArrayList<String>();
            for (Object m : mods) {
                Object id = findMethod(m, "getModId").invoke(m);
                Object v = findMethod(m, "getVersion").invoke(m);
                out.add(id + " " + v);
                if (out.size() >= 600) break;
            }
            return result(name, out);
        } catch (Throwable t) {
            return null;
        }
    }

    private static java.lang.reflect.Method findMethod(Object o, String name) throws NoSuchMethodException {
        for (Class<?> k = o.getClass(); k != null; k = k.getSuperclass()) {
            for (Class<?> i : k.getInterfaces()) {
                try {
                    return i.getMethod(name);
                } catch (NoSuchMethodException ignored) {
                }
            }
        }
        return o.getClass().getMethod(name);
    }

    private static Map<String, Object> result(String loader, List<String> mods) {
        Map<String, Object> m = new LinkedHashMap<String, Object>();
        m.put("loader", loader);
        m.put("count", mods.size());
        m.put("mods", mods);
        return m;
    }
}
