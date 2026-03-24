package com.overmind.api;

/**
 * Generates Overworld terrain.
 *
 * <ul>
 *   <li>y = 0          — bedrock (solid floor)
 *   <li>y = 1–4        — mixed bedrock/stone (random pattern)
 *   <li>y = 5 to surface−4 — stone
 *   <li>y = surface−3 to surface−1 — dirt
 *   <li>y = surface    — grass block (above sea level) or dirt (at/below)
 * </ul>
 */
public class OverworldGenerator implements WorldGenerator {

    private static final int SEA_LEVEL    = 63;
    private static final int BASE_HEIGHT  = 68;
    private static final int HEIGHT_RANGE = 30;

    private final SimplexNoise terrain;
    private final SimplexNoise detail;
    private final SimplexNoise bedrockNoise;

    public OverworldGenerator(long seed) {
        terrain      = new SimplexNoise(seed);
        detail       = new SimplexNoise(seed ^ 0xDEADBEEF12345678L);
        bedrockNoise = new SimplexNoise(seed ^ 0xCAFEBABEDEADC0DEL);
    }

    @Override
    public String getDimensionName() { return "minecraft:overworld"; }

    @Override
    public ChunkNode generate(int chunkX, int chunkZ) {
        ChunkNode chunk = new ChunkNode(chunkX, chunkZ);

        for (int x = 0; x < 16; x++) {
            for (int z = 0; z < 16; z++) {
                double wx = (chunkX * 16 + x) / 128.0;
                double wz = (chunkZ * 16 + z) / 128.0;

                // Multi-octave height
                double n = terrain.octaveNoise(wx, wz, 5, 1.0, 0.5, 2.0);
                double d = detail.octaveNoise(wx * 2, wz * 2, 3, 1.0, 0.5, 2.0) * 0.25;
                int surface = BASE_HEIGHT + (int) ((n + d) * HEIGHT_RANGE);
                surface = Math.max(5, Math.min(surface, 220));

                // Bedrock floor
                chunk.setBlock(x, 0, z, BlockStates.BEDROCK);
                for (int y = 1; y <= 4; y++) {
                    double bv = bedrockNoise.noise(wx * 3 + y, wz * 3 - y);
                    chunk.setBlock(x, y, z, bv > -0.2 ? BlockStates.BEDROCK : BlockStates.STONE);
                }

                // Stone body
                for (int y = 5; y < surface - 3; y++) {
                    chunk.setBlock(x, y, z, BlockStates.STONE);
                }

                // Dirt layer (3 blocks deep)
                for (int y = Math.max(5, surface - 3); y < surface; y++) {
                    chunk.setBlock(x, y, z, BlockStates.DIRT);
                }

                // Surface cap
                if (surface >= 5) {
                    short top = surface > SEA_LEVEL ? BlockStates.GRASS_BLOCK : BlockStates.DIRT;
                    chunk.setBlock(x, surface, z, top);
                }
            }
        }

        chunk.markLoaded();
        return chunk;
    }
}
