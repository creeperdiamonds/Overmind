package com.overmind.java.plugin;

import com.overmind.java.plugin.event.EventBus;
import com.overmind.java.plugin.event.Listener;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Convenience base class for plugins.
 *
 * <p>Plugin authors extend this class and override {@link #onLoad()},
 * {@link #onEnable()}, and/or {@link #onDisable()}.  They should NOT
 * call {@link #init} themselves — that is called by the {@link PluginLoader}.
 */
public abstract class JavaPlugin implements Plugin {

    private PluginMeta    meta;
    private PluginManager pluginManager;
    private EventBus      eventBus;
    private Logger        logger;
    private boolean       enabled;

    /**
     * Called by {@link PluginLoader} immediately after instantiation.
     * Must not be called by plugin code.
     */
    final void init(PluginMeta meta, PluginManager pluginManager, EventBus eventBus) {
        this.meta          = meta;
        this.pluginManager = pluginManager;
        this.eventBus      = eventBus;
        this.logger        = LoggerFactory.getLogger(meta.getName());
    }

    // ── Plugin interface ───────────────────────────────────────────────────

    @Override public void onLoad()    {}
    @Override public void onEnable()  {}
    @Override public void onDisable() {}

    @Override public PluginMeta    getMeta()          { return meta; }
    @Override public boolean       isEnabled()        { return enabled; }
    @Override public PluginManager getPluginManager() { return pluginManager; }

    // ── Helpers for plugin authors ─────────────────────────────────────────

    public Logger getLogger() { return logger; }

    /**
     * Registers all {@link com.overmind.java.plugin.event.EventHandler}-annotated
     * methods on {@code listener} with the shared {@link EventBus}.
     */
    public void registerEvents(Listener listener) {
        eventBus.register(listener);
    }

    // ── Called by PluginManager only ───────────────────────────────────────

    final void setEnabled(boolean enabled) {
        if (this.enabled == enabled) return;
        if (enabled) {
            onEnable();         // call first — if it throws, flag stays false
            this.enabled = true;
        } else {
            this.enabled = false; // mark disabled before onDisable so isEnabled() is consistent
            onDisable();
        }
    }
}
