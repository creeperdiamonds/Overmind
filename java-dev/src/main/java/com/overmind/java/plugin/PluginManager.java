package com.overmind.java.plugin;

import com.overmind.java.plugin.event.Event;
import com.overmind.java.plugin.event.EventBus;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;

/**
 * Discovers, loads, and manages the lifecycle of all plugins.
 *
 * <p>Usage:
 * <pre>{@code
 * PluginManager pm = new PluginManager();
 * pm.loadPlugins(Paths.get("plugins"));
 * pm.enableAll();
 * // … server runs …
 * pm.disableAll();
 * }</pre>
 */
public final class PluginManager {

    private static final Logger logger = LoggerFactory.getLogger(PluginManager.class);

    private final EventBus    eventBus = new EventBus();
    /** Insertion-ordered map of plugin name → plugin instance. */
    private final Map<String, JavaPlugin> plugins = new LinkedHashMap<>();
    private final PluginLoader loader  = new PluginLoader(this, eventBus);

    // ── Loading ────────────────────────────────────────────────────────────

    /**
     * Scans {@code dir} for {@code *.jar} files and loads each as a plugin.
     * Creates the directory if it does not exist.
     */
    public void loadPlugins(Path dir) {
        try {
            Files.createDirectories(dir);
        } catch (IOException e) {
            logger.error("Could not create plugins directory {}", dir, e);
            return;
        }

        List<Path> jars = new ArrayList<>();
        try (Stream<Path> entries = Files.list(dir)) {
            entries.filter(p -> p.toString().endsWith(".jar")).forEach(jars::add);
        } catch (IOException e) {
            logger.error("Could not list plugins directory {}", dir, e);
            return;
        }

        if (jars.isEmpty()) {
            logger.info("No plugins found in {}", dir);
            return;
        }

        for (Path jar : jars) {
            JavaPlugin plugin = loader.load(jar);
            if (plugin != null) {
                plugins.put(plugin.getMeta().getName(), plugin);
            }
        }
        logger.info("Loaded {} plugin(s)", plugins.size());
    }

    // ── Lifecycle ──────────────────────────────────────────────────────────

    /** Enables all loaded plugins in load order. */
    public void enableAll() {
        for (JavaPlugin plugin : plugins.values()) {
            enable(plugin);
        }
    }

    /** Disables all plugins in reverse load order (last loaded, first disabled). */
    public void disableAll() {
        List<JavaPlugin> reversed = new ArrayList<>(plugins.values());
        Collections.reverse(reversed);
        for (JavaPlugin plugin : reversed) {
            disable(plugin);
        }
    }

    public void enable(JavaPlugin plugin) {
        if (plugin.isEnabled()) return;
        try {
            plugin.setEnabled(true);
            logger.info("Enabled plugin {}", plugin.getMeta());
        } catch (Exception e) {
            logger.error("Exception while enabling {}", plugin.getMeta(), e);
        }
    }

    public void disable(JavaPlugin plugin) {
        if (!plugin.isEnabled()) return;
        try {
            plugin.setEnabled(false);
            logger.info("Disabled plugin {}", plugin.getMeta());
        } catch (Exception e) {
            logger.error("Exception while disabling {}", plugin.getMeta(), e);
        }
    }

    // ── Event dispatch ─────────────────────────────────────────────────────

    /** Fires {@code event} through the event bus to all registered handlers. */
    public void callEvent(Event event) {
        eventBus.callEvent(event);
    }

    // ── Queries ────────────────────────────────────────────────────────────

    public JavaPlugin getPlugin(String name) {
        return plugins.get(name);
    }

    public Map<String, JavaPlugin> getPlugins() {
        return Collections.unmodifiableMap(plugins);
    }

    public EventBus getEventBus() {
        return eventBus;
    }
}
