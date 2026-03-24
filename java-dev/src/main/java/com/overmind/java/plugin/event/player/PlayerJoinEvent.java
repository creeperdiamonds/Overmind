package com.overmind.java.plugin.event.player;

import com.overmind.java.plugin.event.Event;

/**
 * Fired when a player successfully completes the login sequence and joins the world.
 */
public final class PlayerJoinEvent extends Event {

    private final String playerName;
    private String joinMessage;

    public PlayerJoinEvent(String playerName, String joinMessage) {
        this.playerName  = playerName;
        this.joinMessage = joinMessage;
    }

    /** The joining player's display name. */
    public String getPlayerName() { return playerName; }

    /** The message broadcast to all players on join. Set to {@code null} to suppress. */
    public String getJoinMessage() { return joinMessage; }

    public void setJoinMessage(String joinMessage) { this.joinMessage = joinMessage; }
}
