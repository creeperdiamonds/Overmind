package com.overmind.java.plugin.event;

/**
 * Mixin interface for events that can be cancelled by a plugin handler.
 *
 * <p>When a handler sets {@code cancelled = true}, subsequent handlers
 * whose {@link EventHandler#ignoreCancelled()} is {@code true} are skipped.
 * {@code MONITOR} priority handlers always run regardless of this flag.
 */
public interface Cancellable {

    boolean isCancelled();

    void setCancelled(boolean cancelled);
}
