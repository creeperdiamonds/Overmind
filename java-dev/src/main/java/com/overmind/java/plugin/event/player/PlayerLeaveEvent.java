package com.overmind.java.plugin.event.player;

import com.overmind.java.plugin.event.Event;

/** Fired when a player disconnects from the server. */
public final class PlayerLeaveEvent extends Event {

    private final String playerName;
    private String leaveMessage;

    public PlayerLeaveEvent(String playerName, String leaveMessage) {
        this.playerName   = playerName;
        this.leaveMessage = leaveMessage;
    }

    public String getPlayerName()  { return playerName; }

    public String getLeaveMessage()                    { return leaveMessage; }
    public void   setLeaveMessage(String leaveMessage) { this.leaveMessage = leaveMessage; }
}
