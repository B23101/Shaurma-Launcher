package dev.shaurma.agent;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** Мінімальний JSON (об'єкти, масиви, рядки, числа, булеві, null) — без зовнішніх залежностей. */
final class Json {
    private final String s;
    private int i;

    private Json(String s) {
        this.s = s;
    }

    /** Розбирає JSON; для некоректного вводу кидає IllegalArgumentException. */
    static Object parse(String text) {
        Json p = new Json(text);
        p.ws();
        Object v = p.value(0);
        p.ws();
        if (p.i != p.s.length()) throw new IllegalArgumentException("зайві символи після JSON на позиції " + p.i);
        return v;
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> parseObject(String text) {
        Object v = parse(text);
        if (!(v instanceof Map)) throw new IllegalArgumentException("очікувався JSON-об'єкт");
        return (Map<String, Object>) v;
    }

    private void ws() {
        while (i < s.length() && Character.isWhitespace(s.charAt(i))) i++;
    }

    private Object value(int depth) {
        if (depth > 32) throw new IllegalArgumentException("завелика вкладеність");
        if (i >= s.length()) throw new IllegalArgumentException("несподіваний кінець");
        char c = s.charAt(i);
        switch (c) {
            case '{':
                return object(depth);
            case '[':
                return array(depth);
            case '"':
                return string();
            case 't':
                return literal("true", Boolean.TRUE);
            case 'f':
                return literal("false", Boolean.FALSE);
            case 'n':
                return literal("null", null);
            default:
                return number();
        }
    }

    private Object literal(String word, Object v) {
        if (!s.startsWith(word, i)) throw new IllegalArgumentException("некоректне значення на позиції " + i);
        i += word.length();
        return v;
    }

    private Object number() {
        int st = i;
        while (i < s.length() && "+-0123456789.eE".indexOf(s.charAt(i)) >= 0) i++;
        if (st == i) throw new IllegalArgumentException("некоректний символ на позиції " + i);
        String t = s.substring(st, i);
        try {
            double d = Double.parseDouble(t);
            if (d == Math.rint(d) && Math.abs(d) < 9e15 && t.indexOf('.') < 0 && t.indexOf('e') < 0 && t.indexOf('E') < 0) {
                return Long.valueOf((long) d);
            }
            return Double.valueOf(d);
        } catch (NumberFormatException e) {
            throw new IllegalArgumentException("некоректне число: " + t);
        }
    }

    private String string() {
        i++; // "
        StringBuilder b = new StringBuilder();
        while (i < s.length()) {
            char c = s.charAt(i++);
            if (c == '"') return b.toString();
            if (c != '\\') {
                b.append(c);
                continue;
            }
            if (i >= s.length()) break;
            char e = s.charAt(i++);
            switch (e) {
                case '"': b.append('"'); break;
                case '\\': b.append('\\'); break;
                case '/': b.append('/'); break;
                case 'b': b.append('\b'); break;
                case 'f': b.append('\f'); break;
                case 'n': b.append('\n'); break;
                case 'r': b.append('\r'); break;
                case 't': b.append('\t'); break;
                case 'u':
                    if (i + 4 > s.length()) throw new IllegalArgumentException("некоректний \\u");
                    b.append((char) Integer.parseInt(s.substring(i, i + 4), 16));
                    i += 4;
                    break;
                default:
                    throw new IllegalArgumentException("некоректне екранування \\" + e);
            }
        }
        throw new IllegalArgumentException("незакритий рядок");
    }

    private List<Object> array(int depth) {
        i++; // [
        List<Object> out = new ArrayList<Object>();
        ws();
        if (i < s.length() && s.charAt(i) == ']') {
            i++;
            return out;
        }
        while (true) {
            ws();
            out.add(value(depth + 1));
            ws();
            if (i >= s.length()) throw new IllegalArgumentException("незакритий масив");
            char c = s.charAt(i++);
            if (c == ']') return out;
            if (c != ',') throw new IllegalArgumentException("очікувалась кома в масиві");
        }
    }

    private Map<String, Object> object(int depth) {
        i++; // {
        Map<String, Object> out = new LinkedHashMap<String, Object>();
        ws();
        if (i < s.length() && s.charAt(i) == '}') {
            i++;
            return out;
        }
        while (true) {
            ws();
            if (i >= s.length() || s.charAt(i) != '"') throw new IllegalArgumentException("очікувався ключ об'єкта");
            String k = string();
            ws();
            if (i >= s.length() || s.charAt(i) != ':') throw new IllegalArgumentException("очікувалось ':'");
            i++;
            ws();
            out.put(k, value(depth + 1));
            ws();
            if (i >= s.length()) throw new IllegalArgumentException("незакритий об'єкт");
            char c = s.charAt(i++);
            if (c == '}') return out;
            if (c != ',') throw new IllegalArgumentException("очікувалась кома в об'єкті");
        }
    }

    // ── Запис ────────────────────────────────────────────────────────────

    static String write(Object v) {
        StringBuilder b = new StringBuilder();
        write(b, v);
        return b.toString();
    }

    @SuppressWarnings("unchecked")
    private static void write(StringBuilder b, Object v) {
        if (v == null) {
            b.append("null");
        } else if (v instanceof Boolean) {
            b.append(v.toString());
        } else if (v instanceof Number) {
            double d = ((Number) v).doubleValue();
            if (Double.isNaN(d) || Double.isInfinite(d)) b.append("null");
            else if (v instanceof Double || v instanceof Float) b.append(d == Math.rint(d) && Math.abs(d) < 9e15 ? Long.toString((long) d) : Double.toString(d));
            else b.append(v.toString());
        } else if (v instanceof String) {
            quote(b, (String) v);
        } else if (v instanceof Map) {
            b.append('{');
            boolean first = true;
            for (Map.Entry<String, Object> e : ((Map<String, Object>) v).entrySet()) {
                if (!first) b.append(',');
                first = false;
                quote(b, e.getKey());
                b.append(':');
                write(b, e.getValue());
            }
            b.append('}');
        } else if (v instanceof Iterable) {
            b.append('[');
            boolean first = true;
            for (Object o : (Iterable<Object>) v) {
                if (!first) b.append(',');
                first = false;
                write(b, o);
            }
            b.append(']');
        } else if (v instanceof Object[]) {
            b.append('[');
            Object[] arr = (Object[]) v;
            for (int k = 0; k < arr.length; k++) {
                if (k > 0) b.append(',');
                write(b, arr[k]);
            }
            b.append(']');
        } else {
            quote(b, v.toString());
        }
    }

    private static void quote(StringBuilder b, String s) {
        b.append('"');
        for (int k = 0; k < s.length(); k++) {
            char c = s.charAt(k);
            switch (c) {
                case '"': b.append("\\\""); break;
                case '\\': b.append("\\\\"); break;
                case '\n': b.append("\\n"); break;
                case '\r': b.append("\\r"); break;
                case '\t': b.append("\\t"); break;
                default:
                    if (c < 0x20) b.append(String.format("\\u%04x", (int) c));
                    else b.append(c);
            }
        }
        b.append('"');
    }
}
