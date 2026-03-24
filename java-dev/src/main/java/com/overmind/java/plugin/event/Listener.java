package com.overmind.java.plugin.event;

/**
 * Marker interface for plugin event listener classes.
 *
 * <p>Implement this interface and annotate methods with {@link EventHandler}
 * to receive events.  Register instances via
 * {@link com.overmind.java.plugin.JavaPlugin#registerEvents(Listener)}.
 */
public interface Listener {
}
