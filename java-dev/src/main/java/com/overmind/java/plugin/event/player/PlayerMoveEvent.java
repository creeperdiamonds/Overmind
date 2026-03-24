package com.overmind.java.plugin.event.player;

import com.overmind.java.plugin.event.Cancellable;
import com.overmind.java.plugin.event.Event;

/**
 * Fired when a player moves to a different position.
 * Cancelling this event teleports the player back to {@code from}.
 */
public final class PlayerMoveEvent extends Event implements Cancellable {

    private final String playerName;
    private final double fromX, fromY, fromZ;
    private double toX, toY, toZ;
    private boolean cancelled;

    public PlayerMoveEvent(String playerName,
                           double fromX, double fromY, double fromZ,
                           double toX,   double toY,   double toZ) {
        this.playerName = playerName;
        this.fromX = fromX; this.fromY = fromY; this.fromZ = fromZ;
        this.toX   = toX;   this.toY   = toY;   this.toZ   = toZ;
    }

    public String getPlayerName() { return playerName; }

    public double getFromX() { return fromX; }
    public double getFromY() { return fromY; }
    public double getFromZ() { return fromZ; }

    public double getToX() { return toX; }
    public double getToY() { return toY; }
    public double getToZ() { return toZ; }

    /** Redirect the player to a custom destination. */
    public void setTo(double x, double y, double z) { toX = x; toY = y; toZ = z; }

    @Override public boolean isCancelled()              { return cancelled; }
    @Override public void    setCancelled(boolean c)    { this.cancelled = c; }
}
