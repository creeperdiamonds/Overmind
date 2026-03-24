package com.overmind.java;

import com.overmind.api.bridge.BridgePlayer;
import com.overmind.api.bridge.PlayerStore;

import java.util.Collection;
import java.util.stream.Collectors;

/**
 * Java-side implementation of {@link PlayerStore}.
 *
 * <p>Bridges the typed Kotlin API (in the {@code overmind-api} module) to the
 * live {@link PlayerRegistry} and the optional {@link DragonflybridgeClient}
 * for Bedrock-targeted actions.
 *
 * <p>Java players receive messages / kicks via their Netty channel through the
 * registry listener mechanism.  Bedrock players are reached via the Dragonfly
 * bridge client.  When {@code bridgeClient} is {@code null} (OVERJAVA mode),
 * Bedrock-targeted actions are silently ignored — the Bedrock UDP handler on
 * the Java side handles those sessions directly.
 */
public class OvermindPlayerStore implements PlayerStore {

    private final PlayerRegistry registry;
    private final DragonflybridgeClient bridgeClient; // null in OVERJAVA mode

    public OvermindPlayerStore(PlayerRegistry registry, DragonflybridgeClient bridgeClient) {
        this.registry    = registry;
        this.bridgeClient = bridgeClient;
    }

    // ─── PlayerStore ──────────────────────────────────────────────────────────

    @Override
    public Collection<BridgePlayer> allPlayers() {
        return registry.getAll().stream()
                .map(OvermindPlayerStore::toSnapshot)
                .collect(Collectors.toUnmodifiableList());
    }

    @Override
    public BridgePlayer getPlayer(String name) {
        PlayerRegistry.PlayerEntry entry = registry.getPlayer(name);
        return entry == null ? null : toSnapshot(entry);
    }

    @Override
    public void kickPlayer(String name, String reason) {
        PlayerRegistry.PlayerEntry entry = registry.getPlayer(name);
        if (entry == null) return;
        if (entry.isBedrock && bridgeClient != null) {
            bridgeClient.kickPlayer(name, reason);
        }
        // Java player kick: unregister cleans up; packet-level disconnect is handled
        // by the PlayHandler when the channel closes after registry removal.
        registry.unregister(name);
    }

    @Override
    public void messagePlayer(String name, String message) {
        PlayerRegistry.PlayerEntry entry = registry.getPlayer(name);
        if (entry == null) return;
        if (entry.isBedrock && bridgeClient != null) {
            bridgeClient.messagePlayer(name, message);
        }
        // Java player messaging via chat packet is handled by the PlayHandler listener
        // registered on the entry. No direct action needed here.
    }

    @Override
    public void broadcast(String message) {
        // Bedrock: single broadcast command fans out to all Bedrock players in Go.
        if (bridgeClient != null) {
            bridgeClient.broadcast(message);
        }
        // Java players are reached through the PlayHandler listener mechanism;
        // no direct action needed here until a chat-packet API is added.
    }

    // ─── Helpers ──────────────────────────────────────────────────────────────

    private static BridgePlayer toSnapshot(PlayerRegistry.PlayerEntry e) {
        return new BridgePlayer(e.username, e.isBedrock, e.x, e.y, e.z, e.yaw, e.pitch);
    }
}
