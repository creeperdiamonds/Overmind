package com.overmind.api.bridge;

import java.util.Collection;

/**
 * Interface boundary between the API module and the java module.
 *
 * <p>Implemented by {@code com.overmind.java.OvermindPlayerStore}, which has access
 * to the live {@code PlayerRegistry} and the Dragonfly bridge client.  Defined here
 * in the API module so that Kotlin plugins and scripts can call it without creating
 * a circular dependency on the java module.
 */
public interface PlayerStore {
    Collection<BridgePlayer> allPlayers();
    BridgePlayer getPlayer(String name);
    void kickPlayer(String name, String reason);
    void messagePlayer(String name, String message);
    void broadcast(String message);
}
