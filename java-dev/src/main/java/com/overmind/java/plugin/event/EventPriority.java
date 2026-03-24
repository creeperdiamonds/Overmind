package com.overmind.java.plugin.event;

/**
 * Determines the order in which plugin handlers receive an event.
 *
 * <p>Handlers run from {@link #LOWEST} to {@link #MONITOR}.
 * {@code MONITOR} handlers should never mutate the event — they are
 * only for observing the final outcome (e.g. logging).
 */
public enum EventPriority {
    /** Run first — highest chance of being overridden by other plugins. */
    LOWEST,
    LOW,
    /** Default priority for most handlers. */
    NORMAL,
    HIGH,
    /** Run last among modifying handlers. */
    HIGHEST,
    /** Read-only observation; always runs even if the event is cancelled. */
    MONITOR
}
