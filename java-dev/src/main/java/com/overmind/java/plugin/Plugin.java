package com.overmind.java.plugin;

/**
 * The root interface for all Overjava-dev plugins.
 *
 * <p>Most plugins extend {@link JavaPlugin} instead of implementing this
 * interface directly, since {@code JavaPlugin} provides logger access,
 * event registration, and automatic lifecycle management.
 */
public interface Plugin {

    /** Called once after the plugin JAR is loaded — before any other plugin is enabled. */
    void onLoad();

    /** Called when the server enables this plugin. Runs after all dependencies are enabled. */
    void onEnable();

    /** Called when the server disables this plugin (e.g. on shutdown). */
    void onDisable();

    /** Returns the metadata parsed from this plugin's {@code plugin.yml}. */
    PluginMeta getMeta();

    /** Returns {@code true} while the plugin is enabled. */
    boolean isEnabled();

    /** Returns the {@link PluginManager} that loaded this plugin. */
    PluginManager getPluginManager();
}
