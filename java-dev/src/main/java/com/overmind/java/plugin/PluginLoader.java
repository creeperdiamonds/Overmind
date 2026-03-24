package com.overmind.java.plugin;

import com.overmind.java.plugin.event.EventBus;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.net.URL;
import java.net.URLClassLoader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.jar.JarFile;
import java.util.zip.ZipEntry;

/**
 * Loads a single plugin JAR file.
 *
 * <p>Each plugin gets its own {@link URLClassLoader} whose parent is the
 * server's class loader, so plugins share the server API but have isolated
 * namespaces from each other.
 */
public final class PluginLoader {

    private static final Logger logger = LoggerFactory.getLogger(PluginLoader.class);

    private final PluginManager pluginManager;
    private final EventBus      eventBus;

    public PluginLoader(PluginManager pluginManager, EventBus eventBus) {
        this.pluginManager = pluginManager;
        this.eventBus      = eventBus;
    }

    /**
     * Loads the plugin at {@code jarPath}.
     *
     * @return the loaded (but not yet enabled) {@link JavaPlugin}, or {@code null} on failure
     */
    public JavaPlugin load(Path jarPath) {
        try (JarFile jar = new JarFile(jarPath.toFile())) {
            PluginMeta meta = parseMeta(jar);
            if (meta == null) {
                logger.error("Plugin JAR {} has no plugin.yml — skipping", jarPath.getFileName());
                return null;
            }

            URLClassLoader loader = new URLClassLoader(
                    new URL[]{jarPath.toUri().toURL()},
                    getClass().getClassLoader());

            try {
                Class<?> clazz = loader.loadClass(meta.getMain());
                if (!JavaPlugin.class.isAssignableFrom(clazz)) {
                    logger.error("Main class {} in {} does not extend JavaPlugin",
                            meta.getMain(), jarPath.getFileName());
                    loader.close();
                    return null;
                }

                JavaPlugin plugin = (JavaPlugin) clazz.getDeclaredConstructor().newInstance();
                plugin.init(meta, pluginManager, eventBus);
                plugin.onLoad();
                logger.info("Loaded plugin {} from {}", meta, jarPath.getFileName());
                return plugin;

            } catch (Exception e) {
                try { loader.close(); } catch (IOException ignored) {}
                throw e; // rethrown to outer catch for logging
            }

        } catch (Exception e) {
            logger.error("Failed to load plugin from {}", jarPath.getFileName(), e);
            return null;
        }
    }

    // ── plugin.yml parser ──────────────────────────────────────────────────

    private PluginMeta parseMeta(JarFile jar) throws IOException {
        ZipEntry entry = jar.getEntry("plugin.yml");
        if (entry == null) return null;

        String name = null, version = null, main = null,
               description = null, apiVersion = null;
        List<String> authors    = new ArrayList<>();
        List<String> depend     = new ArrayList<>();
        List<String> softdepend = new ArrayList<>();

        try (InputStream in  = jar.getInputStream(entry);
             BufferedReader br = new BufferedReader(new InputStreamReader(in, StandardCharsets.UTF_8))) {
            String line;
            while ((line = br.readLine()) != null) {
                line = line.trim();
                if (line.isEmpty() || line.startsWith("#")) continue;

                int colon = line.indexOf(':');
                if (colon < 0) continue;

                String key = line.substring(0, colon).trim().toLowerCase();
                String val = line.substring(colon + 1).trim();

                switch (key) {
                    case "name"        -> name        = val;
                    case "version"     -> version     = val;
                    case "main"        -> main        = val;
                    case "description" -> description = val;
                    case "api-version" -> apiVersion  = val;
                    case "authors"     -> authors     = parseList(val);
                    case "depend"      -> depend      = parseList(val);
                    case "softdepend"  -> softdepend  = parseList(val);
                }
            }
        }

        if (name == null || version == null || main == null) return null;
        return new PluginMeta(name, version, main, description, apiVersion,
                              authors, depend, softdepend);
    }

    /** Parses YAML inline sequence {@code [A, B, C]} or plain {@code A, B, C}. */
    private static List<String> parseList(String val) {
        String stripped = val.replaceAll("[\\[\\]]", "").trim();
        if (stripped.isEmpty()) return new ArrayList<>();
        return new ArrayList<>(Arrays.asList(stripped.split("\\s*,\\s*")));
    }
}
