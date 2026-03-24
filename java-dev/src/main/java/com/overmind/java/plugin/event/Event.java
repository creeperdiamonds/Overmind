package com.overmind.java.plugin.event;

/**
 * Base class for all Overjava-dev plugin events.
 *
 * <p>Extend this class to define a new event type.  If the event can be
 * cancelled by a handler, also implement {@link Cancellable}.
 */
public abstract class Event {

    /** Returns the human-readable name of this event, used in debug output. */
    public String getEventName() {
        return getClass().getSimpleName();
    }
}
