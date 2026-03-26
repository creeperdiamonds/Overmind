package com.overmind.java;

import io.netty.channel.Channel;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.Collection;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Thread-safe registry of every connected player, regardless of protocol.
 *
 * <p>When players join, move, or leave, all other registered players are notified
 * via their {@link PlayerListener} callbacks.  Each listener implementation is
 * responsible for dispatching any resulting packet writes onto its own Netty
 * {@code EventLoop} thread (e.g. via {@code ctx.channel().eventLoop().execute(...)}).
 *
 * <p>Entity IDs start at 2 (1 is reserved for the local player on each client).
 */
public class PlayerRegistry {
    private static final Logger logger = LoggerFactory.getLogger(PlayerRegistry.class);

    private static final AtomicInteger entityIdCounter = new AtomicInteger(2);

    // ─── Listener interface ────────────────────────────────────────────────────

    public interface PlayerListener {
        /** Another player has just entered the world — spawn them. */
        void onPlayerJoined(PlayerEntry joiner);
        /** Another player has disconnected — despawn them. */
        void onPlayerLeft(PlayerEntry leaver);
        /** Another player moved — update their position. */
        void onPlayerMoved(PlayerEntry mover);
    }

    // ─── Player entry ──────────────────────────────────────────────────────────

    public static final class PlayerEntry {
        public final int    entityId;
        public final String username;
        public final boolean isBedrock;

        /** Netty channel — set by PlayHandler after registration; used for kick/message. */
        public volatile Channel channel;

        public volatile double x, y, z;
        public volatile float  yaw, pitch, headYaw;

        final PlayerListener listener;

        PlayerEntry(int entityId, String username, boolean isBedrock, PlayerListener listener) {
            this.entityId  = entityId;
            this.username  = username;
            this.isBedrock = isBedrock;
            this.listener  = listener;
        }
    }

    // ─── State ─────────────────────────────────────────────────────────────────

    private final ConcurrentHashMap<String, PlayerEntry> players = new ConcurrentHashMap<>();

    // ─── API ───────────────────────────────────────────────────────────────────

    /**
     * Registers a new player and notifies all other parties.
     * <ol>
     *   <li>Every existing player is told about the new joiner.</li>
     *   <li>The new joiner is told about every existing player.</li>
     * </ol>
     *
     * @return the {@link PlayerEntry} assigned to this player (stable for the session)
     */
    public PlayerEntry register(String username, boolean isBedrock,
                                double x, double y, double z,
                                PlayerListener listener) {
        PlayerEntry entry = new PlayerEntry(
                entityIdCounter.getAndIncrement(), username, isBedrock, listener);
        entry.x = x;
        entry.y = y;
        entry.z = z;

        // 1. Tell existing players about the new joiner
        for (PlayerEntry existing : players.values()) {
            safeNotify(existing.listener, l -> l.onPlayerJoined(entry));
        }

        players.put(username, entry);

        // 2. Tell the new joiner about each player already online
        for (PlayerEntry existing : players.values()) {
            if (!existing.username.equals(username)) {
                safeNotify(entry.listener, l -> l.onPlayerJoined(existing));
            }
        }

        logger.info("PlayerRegistry: '{}' joined ({}) — {} online",
                username, isBedrock ? "Bedrock" : "Java", players.size());
        return entry;
    }

    /**
     * Removes a player and notifies all remaining players to despawn them.
     */
    public void unregister(String username) {
        PlayerEntry removed = players.remove(username);
        if (removed == null) return;
        for (PlayerEntry other : players.values()) {
            safeNotify(other.listener, l -> l.onPlayerLeft(removed));
        }
        logger.info("PlayerRegistry: '{}' left — {} online", username, players.size());
    }

    /**
     * Updates the stored position of {@code entry} and broadcasts it to all other players.
     */
    public void updatePosition(PlayerEntry entry,
                               double x, double y, double z,
                               float yaw, float pitch, float headYaw) {
        entry.x       = x;
        entry.y       = y;
        entry.z       = z;
        entry.yaw     = yaw;
        entry.pitch   = pitch;
        entry.headYaw = headYaw;

        for (PlayerEntry other : players.values()) {
            if (!other.username.equals(entry.username)) {
                safeNotify(other.listener, l -> l.onPlayerMoved(entry));
            }
        }
    }

    public Collection<PlayerEntry> getAll() {
        return players.values();
    }

    public PlayerEntry getPlayer(String username) {
        return players.get(username);
    }

    public int size() {
        return players.size();
    }

    // ─── Internal helpers ──────────────────────────────────────────────────────

    @FunctionalInterface
    private interface ListenerAction {
        void call(PlayerListener l);
    }

    private static void safeNotify(PlayerListener listener, ListenerAction action) {
        try {
            action.call(listener);
        } catch (Exception e) {
            logger.warn("PlayerRegistry: listener notification failed", e);
        }
    }
}
