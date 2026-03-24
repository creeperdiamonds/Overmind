package com.overmind.api.bridge;

import java.util.Collection;
import java.util.Collections;

/**
 * Global entry-point for player operations across both editions.
 *
 * <p>The java layer calls {@link #install(PlayerStore)} once at startup with an
 * {@code OvermindPlayerStore} implementation.  After that, any code — Java or Kotlin —
 * can call this class directly without importing anything from the {@code java} module.
 *
 * <p>Usage (Java side, called once at startup):
 * <pre>
 * OvermindBridge.INSTANCE.install(new OvermindPlayerStore(registry, bridgeClient));
 * </pre>
 *
 * <p>Usage (any time after install):
 * <pre>
 * OvermindBridge.INSTANCE.getPlayers().forEach(p ->
 *     OvermindBridge.INSTANCE.messagePlayer(p.name(), "Hello!"));
 * </pre>
 */
public final class OvermindBridge {

    public static final OvermindBridge INSTANCE = new OvermindBridge();

    private volatile PlayerStore store;

    private OvermindBridge() {}

    /** Called once by {@code OvermindServer} during startup. */
    public void install(PlayerStore playerStore) {
        this.store = playerStore;
    }

    /** Snapshot of every currently connected player (Java + Bedrock). */
    public Collection<BridgePlayer> getPlayers() {
        PlayerStore s = store;
        return s != null ? s.allPlayers() : Collections.emptyList();
    }

    /** Returns the named player, or {@code null} if they are not online. */
    public BridgePlayer getPlayer(String name) {
        PlayerStore s = store;
        return s != null ? s.getPlayer(name) : null;
    }

    /** Disconnects the named player with a reason shown on their screen. */
    public void kickPlayer(String name, String reason) {
        PlayerStore s = store;
        if (s != null) s.kickPlayer(name, reason);
    }

    /** Sends a private message to the named player. */
    public void messagePlayer(String name, String message) {
        PlayerStore s = store;
        if (s != null) s.messagePlayer(name, message);
    }

    /** Broadcasts a message to every connected player on both editions. */
    public void broadcast(String message) {
        PlayerStore s = store;
        if (s != null) s.broadcast(message);
    }
}
