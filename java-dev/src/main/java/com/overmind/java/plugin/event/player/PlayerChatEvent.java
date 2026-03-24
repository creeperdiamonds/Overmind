package com.overmind.java.plugin.event.player;

import com.overmind.java.plugin.event.Cancellable;
import com.overmind.java.plugin.event.Event;

/**
 * Fired when a player sends a chat message.
 * Cancelling prevents the message from being broadcast.
 */
public final class PlayerChatEvent extends Event implements Cancellable {

    private final String playerName;
    private String message;
    private String format;
    private boolean cancelled;

    public PlayerChatEvent(String playerName, String message, String format) {
        this.playerName = playerName;
        this.message    = message;
        this.format     = format;
    }

    public String getPlayerName() { return playerName; }

    public String getMessage()              { return message; }
    public void   setMessage(String msg)    { this.message = msg; }

    /** Broadcast format string, e.g. {@code "<%s> %s"} where args are name, message. */
    public String getFormat()               { return format; }
    public void   setFormat(String format)  { this.format = format; }

    @Override public boolean isCancelled()           { return cancelled; }
    @Override public void    setCancelled(boolean c) { this.cancelled = c; }
}
