package com.overmind.api;

/**
 * Generates The End terrain.
 *
 * <ul>
 *   <li>Main island — end stone dome centred on (0, 0), radius ~60 blocks.
 *   <li>Outer islands — scattered end stone platforms between radius 400–1500.
 *   <li>Everything else — void (all air).
 * </ul>
 */
public class EndGenerator implements WorldGenerator {

    private static final double MAIN_ISLAND_RADIUS = 60.0;

    private final SimplexNoise island;
    private final SimplexNoise outer;

    public EndGenerator(long seed) {
        island = new SimplexNoise(seed ^ 0xEEEEEEEEEEEEEEEEL);
        outer  = new SimplexNoise(seed ^ 0x0FFFFFFFFFFFFFFFL);
    }

    @Override
    public String getDimensionName() { return "minecraft:the_end"; }

    @Override
    public ChunkNode generate(int chunkX, int chunkZ) {
        ChunkNode chunk = new ChunkNode(chunkX, chunkZ);

        for (int x = 0; x < 16; x++) {
            for (int z = 0; z < 16; z++) {
                int worldX = chunkX * 16 + x;
                int worldZ = chunkZ * 16 + z;
                double dist = Math.sqrt((double) worldX * worldX + (double) worldZ * worldZ);

                if (dist <= MAIN_ISLAND_RADIUS) {
                    // Main island — noise-shaped dome, falls off at the edges
                    double wx = worldX / 32.0;
                    double wz = worldZ / 32.0;
                    double n = island.octaveNoise(wx, wz, 4, 1.0, 0.5, 2.0);
                    double falloff = 1.0 - (dist / MAIN_ISLAND_RADIUS);
                    int top = 63 + (int) (n * 10 * falloff);
                    top = Math.max(55, Math.min(top, 75));
                    for (int y = 50; y <= top; y++) {
                        chunk.setBlock(x, y, z, BlockStates.END_STONE);
                    }
                } else if (dist > 400 && dist < 1500) {
                    // Outer islands — scattered end stone patches
                    double wx = worldX / 48.0;
                    double wz = worldZ / 48.0;
                    double n = outer.octaveNoise(wx, wz, 3, 1.0, 0.5, 2.0);
                    if (n > 0.4) {
                        int top = 55 + (int) (n * 15);
                        for (int y = 48; y <= top; y++) {
                            chunk.setBlock(x, y, z, BlockStates.END_STONE);
                        }
                    }
                }
                // else: void (air = 0, default)
            }
        }

        chunk.markLoaded();
        return chunk;
    }
}
