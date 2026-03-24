package com.overmind.api;

/**
 * Generates Nether terrain.
 *
 * <ul>
 *   <li>y = 0–4        — bedrock floor (random pattern)
 *   <li>y = 5–30       — dense netherrack (below lava sea)
 *   <li>y = 31         — lava sea level (stone proxy; update to BlockStates.LAVA once ID is confirmed)
 *   <li>y = 32–90      — netherrack with 3D cave hollows
 *   <li>y = 91–122     — dense netherrack ceiling
 *   <li>y = 123–127    — bedrock ceiling (random pattern)
 * </ul>
 */
public class NetherGenerator implements WorldGenerator {

    private static final int  LAVA_LEVEL = 31;
    private static final double CAVE_THRESHOLD = 0.35;

    private final SimplexNoise cave1;
    private final SimplexNoise cave2;
    private final SimplexNoise bedrockNoise;

    public NetherGenerator(long seed) {
        cave1        = new SimplexNoise(seed ^ 0x1111111111111111L);
        cave2        = new SimplexNoise(seed ^ 0x2222222222222222L);
        bedrockNoise = new SimplexNoise(seed ^ 0x3333333333333333L);
    }

    @Override
    public String getDimensionName() { return "minecraft:the_nether"; }

    @Override
    public ChunkNode generate(int chunkX, int chunkZ) {
        ChunkNode chunk = new ChunkNode(chunkX, chunkZ);

        for (int x = 0; x < 16; x++) {
            for (int z = 0; z < 16; z++) {
                double wx = (chunkX * 16 + x) / 64.0;
                double wz = (chunkZ * 16 + z) / 64.0;

                // Bedrock floor (y=0–4)
                chunk.setBlock(x, 0, z, BlockStates.BEDROCK);
                for (int y = 1; y <= 4; y++) {
                    double bv = bedrockNoise.noise(wx * 2 + y, wz * 2);
                    chunk.setBlock(x, y, z, bv > -0.3 ? BlockStates.BEDROCK : BlockStates.NETHERRACK);
                }

                // Bedrock ceiling (y=123–127)
                chunk.setBlock(x, 127, z, BlockStates.BEDROCK);
                for (int y = 123; y <= 126; y++) {
                    double bv = bedrockNoise.noise(wx * 2 + y * 0.5, wz * 2 + 50);
                    chunk.setBlock(x, y, z, bv > -0.3 ? BlockStates.BEDROCK : BlockStates.NETHERRACK);
                }

                // Interior: netherrack body with cave hollow cut-outs
                for (int y = 5; y <= 122; y++) {
                    double wy  = y / 32.0;
                    double n1  = cave1.noise(wx, wy + wz * 0.3);
                    double n2  = cave2.noise(wx * 1.3 + 100, wy * 0.7 + wz);
                    boolean isCave = (n1 * n1 + n2 * n2) < (CAVE_THRESHOLD * CAVE_THRESHOLD);

                    if (!isCave) {
                        chunk.setBlock(x, y, z, BlockStates.NETHERRACK);
                    } else if (y <= LAVA_LEVEL) {
                        // Stone proxy for lava until BlockStates.LAVA ID is confirmed
                        chunk.setBlock(x, y, z, BlockStates.STONE);
                    }
                    // else: cave air (0)
                }
            }
        }

        chunk.markLoaded();
        return chunk;
    }
}
