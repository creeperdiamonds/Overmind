package com.overmind.api;

/**
 * Procedurally generates chunk data for a specific dimension.
 */
public interface WorldGenerator {

    /**
     * Generates all blocks for the chunk at the given coordinates.
     * The returned {@link ChunkNode} has {@link ChunkNode#isLoaded()} == true.
     */
    ChunkNode generate(int chunkX, int chunkZ);

    /** Minecraft dimension identifier, e.g. {@code "minecraft:overworld"}. */
    String getDimensionName();
}
