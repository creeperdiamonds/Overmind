package com.overmind.api;

/**
 * A single bone parsed from a Bedrock {@code .geo.json} model file.
 *
 * <p>Coordinate system: X right, Y up, Z toward viewer (Bedrock model space).
 * Pivot is in model units (1 unit = 1/16 of a Minecraft block).
 * Rotation angles are in degrees (Euler XYZ order applied per Bedrock spec).
 */
public final class GeoModelBone {

    /** Bone identifier (matches animation JSON keys). */
    public final String name;

    /** Parent bone name, or {@code null} for a root bone. */
    public final String parent;

    // Pivot point — origin of this bone's local rotation
    public final float pivotX;
    public final float pivotY;
    public final float pivotZ;

    // Bind-pose rotation (degrees, Euler XYZ)
    public final float rotX;
    public final float rotY;
    public final float rotZ;

    public GeoModelBone(String name, String parent,
                        float pivotX, float pivotY, float pivotZ,
                        float rotX,   float rotY,   float rotZ) {
        this.name   = name;
        this.parent = parent;
        this.pivotX = pivotX;
        this.pivotY = pivotY;
        this.pivotZ = pivotZ;
        this.rotX   = rotX;
        this.rotY   = rotY;
        this.rotZ   = rotZ;
    }

    @Override
    public String toString() {
        return "GeoModelBone{name='" + name + "', parent='" + parent + "'}";
    }
}
