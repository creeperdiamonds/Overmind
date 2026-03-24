package com.overmind.api;

import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;

public class ChunkNode {
    /** Full Java Edition world height (384 blocks, y = 0..383). */
    public static final int JAVA_HEIGHT = 384;
    /** Usable Bedrock Edition world height (383 blocks, y = 0..382; y=383 is stripped). */
    public static final int BEDROCK_HEIGHT = 383;
    /** Chunk sections per Java client (JAVA_HEIGHT / 16). */
    public static final int JAVA_SECTION_COUNT = JAVA_HEIGHT / 16; // 24
    /** Chunk sections per Bedrock client (same 24 sections; section 23 top-layer stripped). */
    public static final int BEDROCK_SECTION_COUNT = JAVA_SECTION_COUNT; // 24

    private final int chunkX;
    private final int chunkZ;
    private final short[][][] blocks;
    private final AtomicInteger loadLevel;
    private final AtomicBoolean isDirty;
    private final AtomicBoolean isLoaded;
    private final long lastModified;
    
    public ChunkNode(int chunkX, int chunkZ) {
        this.chunkX = chunkX;
        this.chunkZ = chunkZ;
        this.blocks = new short[16][384][16];
        this.loadLevel = new AtomicInteger(0);
        this.isDirty = new AtomicBoolean(false);
        this.isLoaded = new AtomicBoolean(false);
        this.lastModified = System.currentTimeMillis();
    }
    
    public int getChunkX() {
        return chunkX;
    }
    
    public int getChunkZ() {
        return chunkZ;
    }
    
    public short getBlock(int x, int y, int z) {
        if (x < 0 || x >= 16 || y < 0 || y >= 384 || z < 0 || z >= 16) {
            return 0;
        }
        return blocks[x][y][z];
    }
    
    public void setBlock(int x, int y, int z, short blockId) {
        if (x < 0 || x >= 16 || y < 0 || y >= 384 || z < 0 || z >= 16) {
            return;
        }
        blocks[x][y][z] = blockId;
        isDirty.set(true);
    }

    /**
     * Returns the block at (x, y, z) as seen by a Java Edition client.
     * y = 383 is always reported as air (0) — the injected fake layer that
     * gives Java clients their expected 384-block world height on a
     * Bedrock-primary (383-block) server.
     */
    public short getBlockForJavaClient(int x, int y, int z) {
        if (y == JAVA_HEIGHT - 1) return 0; // injected fake-air layer at y=383
        return getBlock(x, y, z);
    }

    /**
     * Returns the block at (x, y, z) as seen by a Bedrock Edition client.
     * y >= 383 is always reported as air (0) — the stripped layer that
     * prevents Bedrock clients from receiving blocks outside their
     * native world-height range.
     */
    public short getBlockForBedrockClient(int x, int y, int z) {
        if (y >= BEDROCK_HEIGHT) return 0; // strip y=383 and above
        return getBlock(x, y, z);
    }

    public void ensure383Limit() {
        for (int x = 0; x < 16; x++) {
            for (int z = 0; z < 16; z++) {
                if (blocks[x][383][z] != 0) {
                    blocks[x][383][z] = 0;
                    isDirty.set(true);
                }
            }
        }
    }
    
    public int getLoadLevel() {
        return loadLevel.get();
    }
    
    public void incrementLoadLevel() {
        loadLevel.incrementAndGet();
    }
    
    public void decrementLoadLevel() {
        loadLevel.decrementAndGet();
    }
    
    public boolean isDirty() {
        return isDirty.get();
    }
    
    public void markClean() {
        isDirty.set(false);
    }
    
    public boolean isLoaded() {
        return isLoaded.get();
    }
    
    public void markLoaded() {
        isLoaded.set(true);
    }
    
    public void markUnloaded() {
        isLoaded.set(false);
    }
    
    public long getLastModified() {
        return lastModified;
    }
    
    public short[][][] getBlocks() {
        return blocks;
    }
    
    @Override
    public boolean equals(Object obj) {
        if (this == obj) return true;
        if (obj == null || getClass() != obj.getClass()) return false;
        ChunkNode chunkNode = (ChunkNode) obj;
        return chunkX == chunkNode.chunkX && chunkZ == chunkNode.chunkZ;
    }
    
    @Override
    public int hashCode() {
        return 31 * chunkX + chunkZ;
    }
    
    @Override
    public String toString() {
        return "ChunkNode{" +
                "chunkX=" + chunkX +
                ", chunkZ=" + chunkZ +
                ", loaded=" + isLoaded.get() +
                ", dirty=" + isDirty.get() +
                '}';
    }
}
