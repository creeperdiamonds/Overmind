package com.overmind.api;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.HashMap;
import java.util.Map;

/**
 * Minimal TOML reader for {@code overmind.toml}.
 *
 * <p>Handles the subset used by this project: {@code [section]} /
 * {@code [section.subsection]} headers, string / integer / boolean scalar
 * values, and {@code #} comments.  Only the {@code [java]}, {@code [java.world]},
 * and {@code [bridge]} sections are read; all other sections are ignored.
 */
public final class OvermindConfig {

    // ── [java] ────────────────────────────────────────────────────────────────

    /** TCP port for Java Edition clients. */
    public int     port         = 25565;

    /**
     * Server mode: {@code "overmind"} (Bedrock-primary, Go engine owns :19132)
     * or {@code "overjava"} (Java-primary, Java server owns :19132 directly).
     */
    public String  mode         = "overmind";

    /** World generation seed. */
    public long    seed         = 12345L;

    /** Chunk view distance sent to Java clients. */
    public int     viewDistance = 8;

    /** Maximum simultaneous Java Edition players. */
    public int     maxPlayers   = 100;

    // ── [java.world] ──────────────────────────────────────────────────────────

    /** Whether to persist the Java world to disk. */
    public boolean worldSaveData = true;

    /** Directory for the Java world (LevelDB). */
    public String  worldFolder   = "server/java";

    // ── [bridge] ──────────────────────────────────────────────────────────────

    /**
     * Internal TCP address of the Go→Java event bridge.
     * Format: {@code "host:port"} or {@code ":port"}.
     */
    public String  bridgeAddress = ":25566";


    // ── Factory ───────────────────────────────────────────────────────────────

    /** Creates an instance pre-loaded with all default values. */
    public OvermindConfig() {}

    /**
     * Load configuration from {@code path}.  If the file does not exist the
     * returned object contains all default values.
     *
     * @throws IOException if the file exists but cannot be read or parsed.
     */
    public static OvermindConfig load(Path path) throws IOException {
        OvermindConfig cfg = new OvermindConfig();
        if (!Files.exists(path)) {
            return cfg;
        }
        Map<String, Map<String, String>> sections = parse(path);
        apply(cfg, sections);
        return cfg;
    }

    // ── Derived helpers ───────────────────────────────────────────────────────

    /**
     * Extract the host portion of {@link #bridgeAddress}.
     * {@code ":25566"} → {@code "localhost"}; {@code "host:25566"} → {@code "host"}.
     */
    public String bridgeHost() {
        if (bridgeAddress.startsWith(":")) return "localhost";
        int idx = bridgeAddress.lastIndexOf(':');
        return idx > 0 ? bridgeAddress.substring(0, idx) : "localhost";
    }

    /**
     * Extract the port portion of {@link #bridgeAddress}.
     * Falls back to {@code 25566} on parse error.
     */
    public int bridgePort() {
        int idx = bridgeAddress.lastIndexOf(':');
        if (idx < 0) return 25566;
        try {
            return Integer.parseInt(bridgeAddress.substring(idx + 1).trim());
        } catch (NumberFormatException e) {
            return 25566;
        }
    }

    /** True when mode is {@code "overmind"} (Bedrock-primary). */
    public boolean isOvermindMode() {
        return "overmind".equalsIgnoreCase(mode.trim());
    }

    // ── Parser ────────────────────────────────────────────────────────────────

    /**
     * Parse a TOML file into a map of {@code sectionName → (key → rawValue)}.
     * Raw values are stripped of surrounding quotes and inline comments.
     */
    private static Map<String, Map<String, String>> parse(Path path) throws IOException {
        Map<String, Map<String, String>> sections = new HashMap<>();
        String currentSection = "";
        for (String raw : Files.readAllLines(path, StandardCharsets.UTF_8)) {
            String line = raw.trim();
            if (line.isEmpty() || line.startsWith("#")) continue;

            if (line.startsWith("[")) {
                // Section header — strip brackets and any trailing inline comment.
                int end = line.indexOf(']');
                if (end > 0) {
                    currentSection = line.substring(1, end).trim();
                }
            } else if (line.contains("=")) {
                int eq = line.indexOf('=');
                String key   = line.substring(0, eq).trim();
                String value = normaliseValue(line.substring(eq + 1).trim());
                sections.computeIfAbsent(currentSection, k -> new HashMap<>())
                        .put(key, value);
            }
        }
        return sections;
    }

    /**
     * Strip surrounding quotes from a string value, or remove trailing inline
     * {@code #} comments from unquoted scalars.
     */
    private static String normaliseValue(String raw) {
        if (raw.startsWith("\"")) {
            // Quoted string — return content between first pair of quotes.
            int close = raw.indexOf('"', 1);
            return close > 0 ? raw.substring(1, close) : raw.substring(1);
        }
        if (raw.startsWith("'")) {
            int close = raw.indexOf('\'', 1);
            return close > 0 ? raw.substring(1, close) : raw.substring(1);
        }
        // Unquoted — strip trailing inline comment.
        int hash = raw.indexOf('#');
        return hash >= 0 ? raw.substring(0, hash).trim() : raw;
    }

    // ── Config application ────────────────────────────────────────────────────

    private static void apply(OvermindConfig cfg, Map<String, Map<String, String>> sections) {
        Map<String, String> java      = sections.getOrDefault("java",       new HashMap<>());
        Map<String, String> javaWorld = sections.getOrDefault("java.world", new HashMap<>());
        Map<String, String> bridge    = sections.getOrDefault("bridge",     new HashMap<>());

        cfg.port         = intVal(java,  "port",          cfg.port);
        cfg.mode         = strVal(java,  "mode",          cfg.mode);
        cfg.seed         = longVal(java, "seed",          cfg.seed);
        cfg.viewDistance = intVal(java,  "view_distance", cfg.viewDistance);
        cfg.maxPlayers   = intVal(java,  "max_players",   cfg.maxPlayers);

        cfg.worldSaveData = boolVal(javaWorld, "save_data", cfg.worldSaveData);
        cfg.worldFolder   = strVal(javaWorld,  "folder",    cfg.worldFolder);

        cfg.bridgeAddress = strVal(bridge, "address", cfg.bridgeAddress);
    }

    // ── Value helpers ─────────────────────────────────────────────────────────

    private static String strVal(Map<String, String> m, String key, String def) {
        String v = m.get(key);
        return v != null && !v.isEmpty() ? v : def;
    }

    private static int intVal(Map<String, String> m, String key, int def) {
        try { return Integer.parseInt(m.getOrDefault(key, String.valueOf(def)).trim()); }
        catch (NumberFormatException e) { return def; }
    }

    private static long longVal(Map<String, String> m, String key, long def) {
        try { return Long.parseLong(m.getOrDefault(key, String.valueOf(def)).trim()); }
        catch (NumberFormatException e) { return def; }
    }

    private static boolean boolVal(Map<String, String> m, String key, boolean def) {
        String v = m.get(key);
        return v != null ? Boolean.parseBoolean(v.trim()) : def;
    }
}
