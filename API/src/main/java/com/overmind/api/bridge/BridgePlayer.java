package com.overmind.api.bridge;

import java.util.Objects;

/**
 * Immutable snapshot of a connected player, valid at the moment it was fetched.
 * Covers both Java Edition and Bedrock Edition players.
 */
public final class BridgePlayer {

    private final String  name;
    private final boolean isBedrock;
    private final double  x, y, z;
    private final float   yaw, pitch;

    public BridgePlayer(String name, boolean isBedrock,
                        double x, double y, double z,
                        float yaw, float pitch) {
        this.name      = name;
        this.isBedrock = isBedrock;
        this.x         = x;
        this.y         = y;
        this.z         = z;
        this.yaw       = yaw;
        this.pitch     = pitch;
    }

    public String  name()      { return name; }
    public boolean isBedrock() { return isBedrock; }
    public double  x()         { return x; }
    public double  y()         { return y; }
    public double  z()         { return z; }
    public float   yaw()       { return yaw; }
    public float   pitch()     { return pitch; }

    @Override
    public boolean equals(Object o) {
        if (!(o instanceof BridgePlayer)) return false;
        BridgePlayer other = (BridgePlayer) o;
        return isBedrock == other.isBedrock
                && Double.compare(x, other.x) == 0
                && Double.compare(y, other.y) == 0
                && Double.compare(z, other.z) == 0
                && Float.compare(yaw, other.yaw) == 0
                && Float.compare(pitch, other.pitch) == 0
                && Objects.equals(name, other.name);
    }

    @Override
    public int hashCode() {
        return Objects.hash(name, isBedrock, x, y, z, yaw, pitch);
    }

    @Override
    public String toString() {
        return "BridgePlayer{name=" + name + ", bedrock=" + isBedrock
                + ", x=" + x + ", y=" + y + ", z=" + z + "}";
    }
}
