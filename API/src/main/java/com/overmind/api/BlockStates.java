package com.overmind.api;

/**
 * Minecraft 1.21.4 (protocol 774) global block-state IDs.
 *
 * <p>IDs follow the vanilla block registry order baked into the client.
 * AIR and STONE are guaranteed correct.  Others are best estimates based on the
 * vanilla registration sequence; if a block renders with the wrong texture,
 * compare against a vanilla 1.21.4 server's
 * {@code generated/reports/blocks.json} and update the constant here.
 */
public final class BlockStates {

    /** Air (guaranteed). */
    public static final short AIR         = 0;

    /** Stone (guaranteed). */
    public static final short STONE       = 1;

    /** Grass block [snowy=false] (high confidence). */
    public static final short GRASS_BLOCK = 8;

    /** Dirt (high confidence). */
    public static final short DIRT        = 10;

    /**
     * Bedrock (estimated — comes after all sapling/propagule states in the
     * vanilla registration order, around ID 52 for 1.21.4).
     */
    public static final short BEDROCK     = 52;

    /** Sand (estimated). */
    public static final short SAND        = 66;

    /** Gravel (estimated). */
    public static final short GRAVEL      = 68;

    /** Netherrack (estimated; nether blocks register late in the ID space). */
    public static final short NETHERRACK  = 3571;

    /** End stone (estimated). */
    public static final short END_STONE   = 4039;

    /** Obsidian (estimated). */
    public static final short OBSIDIAN    = 1063;

    private BlockStates() {}
}
