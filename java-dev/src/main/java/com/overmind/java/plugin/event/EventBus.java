package com.overmind.java.plugin.event;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Reflection-based event dispatcher.
 *
 * <p>Handlers are pre-sorted by {@link EventPriority} at registration time so
 * that {@link #callEvent(Event)} is a tight loop with no sorting overhead.
 */
public final class EventBus {

    private static final Logger logger = LoggerFactory.getLogger(EventBus.class);

    /** A single registered handler method together with its metadata. */
    private record HandlerEntry(Listener listener, Method method,
                                EventPriority priority, boolean ignoreCancelled) {}

    /** eventClass → sorted list of handlers (sorted by priority ordinal). */
    private final Map<Class<? extends Event>, List<HandlerEntry>> handlers =
            new ConcurrentHashMap<>();

    /**
     * Scans {@code listener} for {@link EventHandler}-annotated methods and
     * registers each one.  Methods must accept exactly one {@link Event} subclass
     * parameter; others are silently skipped.
     */
    public void register(Listener listener) {
        for (Method method : listener.getClass().getMethods()) {
            EventHandler annotation = method.getAnnotation(EventHandler.class);
            if (annotation == null) continue;

            Class<?>[] params = method.getParameterTypes();
            if (params.length != 1 || !Event.class.isAssignableFrom(params[0])) continue;

            @SuppressWarnings("unchecked")
            Class<? extends Event> eventType = (Class<? extends Event>) params[0];

            HandlerEntry entry = new HandlerEntry(
                    listener, method, annotation.priority(), annotation.ignoreCancelled());

            handlers.computeIfAbsent(eventType, k -> new ArrayList<>()).add(entry);
            handlers.get(eventType).sort(Comparator.comparingInt(e -> e.priority().ordinal()));
        }
    }

    /**
     * Dispatches {@code event} to all registered handlers in priority order.
     *
     * <p>If the event implements {@link Cancellable} and a handler has
     * {@code ignoreCancelled = true}, that handler is skipped once the event is
     * cancelled — except for {@link EventPriority#MONITOR} handlers which always run.
     */
    public void callEvent(Event event) {
        List<HandlerEntry> list = handlers.get(event.getClass());
        if (list == null) return;

        for (HandlerEntry entry : list) {
            if (entry.ignoreCancelled()
                    && entry.priority() != EventPriority.MONITOR
                    && event instanceof Cancellable c
                    && c.isCancelled()) {
                continue;
            }
            try {
                entry.method().invoke(entry.listener(), event);
            } catch (Exception ex) {
                // Log and continue — one bad handler must not block the rest.
                logger.error("Exception in event handler {}.{} for {}",
                        entry.method().getDeclaringClass().getName(),
                        entry.method().getName(),
                        event.getEventName(), ex);
            }
        }
    }
}
