package com.overmind.java.plugin.event;

import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/**
 * Marks a {@link Listener} method as an event handler.
 *
 * <pre>{@code
 * @EventHandler(priority = EventPriority.HIGH)
 * public void onPlayerJoin(PlayerJoinEvent event) {
 *     event.getPlayer().sendMessage("Welcome!");
 * }
 * }</pre>
 *
 * <p>The method must have exactly one parameter whose type extends {@link Event}.
 */
@Retention(RetentionPolicy.RUNTIME)
@Target(ElementType.METHOD)
public @interface EventHandler {

    /** The priority at which this handler runs. Defaults to {@link EventPriority#NORMAL}. */
    EventPriority priority() default EventPriority.NORMAL;

    /**
     * If {@code true}, this handler is skipped when the event has already
     * been cancelled by a lower-priority handler.
     * {@link EventPriority#MONITOR} handlers ignore this flag and always run.
     */
    boolean ignoreCancelled() default false;
}
