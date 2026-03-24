package com.overmind.api;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.util.List;
import java.util.Set;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

public class VertexGraphManager {
    private static final Logger logger = LoggerFactory.getLogger(VertexGraphManager.class);

    private final VertexGraph vertexGraph;
    private final ScheduledExecutorService cleanupExecutor;
    private final int viewDistance;
    private final boolean isOvermindMode;

    // Optional: null means no persistent storage / no generation
    private final WorldStorage   storage;
    private final WorldGenerator generator;

    // -------------------------------------------------------------------------
    // Constructors
    // -------------------------------------------------------------------------

    /**
     * Full constructor with world generation and storage.
     *
     * @param viewDistance  chunk render distance
     * @param isOvermindMode true = Bedrock-Primary (enforce 383-block height cap)
     * @param storage        disk persistence; may be {@code null}
     * @param generator      terrain generator; may be {@code null} (empty chunks)
     */
    public VertexGraphManager(int viewDistance, boolean isOvermindMode,
                              WorldStorage storage, WorldGenerator generator) {
        this.viewDistance  = viewDistance;
        this.isOvermindMode = isOvermindMode;
        this.storage       = storage;
        this.generator     = generator;
        this.vertexGraph   = new VertexGraph(viewDistance);
        this.cleanupExecutor = Executors.newSingleThreadScheduledExecutor(r -> {
            Thread t = new Thread(r, "overmind-world-scheduler");
            t.setDaemon(true);
            return t;
        });

        logger.info("VertexGraphManager — mode: {}, viewDistance: {}, generator: {}, storage: {}",
                isOvermindMode ? "Overmind(Bedrock-Primary)" : "Java-Primary",
                viewDistance,
                generator != null ? generator.getDimensionName() : "none",
                storage   != null ? "enabled" : "disabled");

        startCleanupTask();
        if (storage != null) startSaveTask();
    }

    /** Backward-compatible constructor (no generator, no storage — empty chunks). */
    public VertexGraphManager(int viewDistance, boolean isOvermindMode) {
        this(viewDistance, isOvermindMode, null, null);
    }

    // -------------------------------------------------------------------------
    // Chunk access
    // -------------------------------------------------------------------------

    public ChunkNode getChunk(int chunkX, int chunkZ) {
        return vertexGraph.getChunk(chunkX, chunkZ);
    }

    public ChunkNode getOrCreateChunk(int chunkX, int chunkZ) {
        ChunkNode chunk = vertexGraph.getOrCreateChunk(chunkX, chunkZ);
        if (isOvermindMode) chunk.ensure383Limit();
        return chunk;
    }

    /**
     * Loads a chunk: tries disk storage first, then generates, then falls back to empty.
     * Generated chunks are saved to disk immediately.
     */
    public void loadChunk(int chunkX, int chunkZ) {
        // Already loaded → skip
        ChunkNode existing = vertexGraph.getChunk(chunkX, chunkZ);
        if (existing != null && existing.isLoaded()) return;

        // 1. Try storage
        if (storage != null && storage.hasChunk(chunkX, chunkZ)) {
            try {
                ChunkNode chunk = storage.loadChunk(chunkX, chunkZ);
                if (isOvermindMode) chunk.ensure383Limit();
                vertexGraph.putChunk(chunk);
                return;
            } catch (IOException e) {
                logger.warn("Failed to load chunk ({},{}) from disk — regenerating: {}", chunkX, chunkZ, e.getMessage());
            }
        }

        // 2. Generate
        if (generator != null) {
            ChunkNode chunk = generator.generate(chunkX, chunkZ);
            if (isOvermindMode) chunk.ensure383Limit();
            vertexGraph.putChunk(chunk);
            if (storage != null) {
                try {
                    storage.saveChunk(chunk);
                } catch (IOException e) {
                    logger.warn("Failed to save generated chunk ({},{}): {}", chunkX, chunkZ, e.getMessage());
                }
            }
            return;
        }

        // 3. Fallback: empty chunk (legacy behaviour)
        vertexGraph.loadChunk(chunkX, chunkZ);
        if (isOvermindMode) ensure383LimitForChunk(chunkX, chunkZ);
    }

    public void unloadChunk(int chunkX, int chunkZ) {
        vertexGraph.unloadChunk(chunkX, chunkZ);
    }

    public Set<ChunkNode> getNeighbors(int chunkX, int chunkZ) {
        return vertexGraph.getNeighbors(chunkX, chunkZ);
    }

    public List<ChunkNode> getLoadedChunks() {
        return vertexGraph.getLoadedChunks();
    }

    public void setBlock(int chunkX, int chunkZ, int x, int y, int z, short blockId) {
        ChunkNode chunk = getOrCreateChunk(chunkX, chunkZ);
        if (isOvermindMode && y >= 383) {
            logger.warn("Blocked setBlock at y={} in Overmind mode", y);
            return;
        }
        chunk.setBlock(x, y, z, blockId);
    }

    public short getBlock(int chunkX, int chunkZ, int x, int y, int z) {
        ChunkNode chunk = getChunk(chunkX, chunkZ);
        if (chunk == null) return 0;
        if (isOvermindMode && y >= 383) return 0;
        return chunk.getBlock(x, y, z);
    }

    public void ensure383Limit() {
        if (isOvermindMode) vertexGraph.ensure383Limit();
    }

    public void loadArea(int centerX, int centerZ, int radius) {
        logger.info("Loading area ({},{}) r={}", centerX, centerZ, radius);
        for (int x = centerX - radius; x <= centerX + radius; x++)
            for (int z = centerZ - radius; z <= centerZ + radius; z++)
                loadChunk(x, z);
    }

    public void unloadArea(int centerX, int centerZ, int radius) {
        for (int x = centerX - radius; x <= centerX + radius; x++)
            for (int z = centerZ - radius; z <= centerZ + radius; z++)
                unloadChunk(x, z);
    }

    // -------------------------------------------------------------------------
    // Background tasks
    // -------------------------------------------------------------------------

    private void startCleanupTask() {
        cleanupExecutor.scheduleAtFixedRate(
                vertexGraph::cleanup, 30, 30, TimeUnit.SECONDS);
    }

    /** Periodically flushes dirty chunks to storage. */
    private void startSaveTask() {
        cleanupExecutor.scheduleAtFixedRate(() -> {
            for (ChunkNode chunk : vertexGraph.getLoadedChunks()) {
                if (chunk.isDirty()) {
                    try {
                        storage.saveChunk(chunk);
                    } catch (IOException e) {
                        logger.warn("Auto-save failed ({},{}): {}",
                                chunk.getChunkX(), chunk.getChunkZ(), e.getMessage());
                    }
                }
            }
        }, 60, 60, TimeUnit.SECONDS);
    }

    private void ensure383LimitForChunk(int chunkX, int chunkZ) {
        ChunkNode chunk = getChunk(chunkX, chunkZ);
        if (chunk != null) {
            chunk.ensure383Limit();
            for (ChunkNode n : getNeighbors(chunkX, chunkZ)) n.ensure383Limit();
        }
    }

    // -------------------------------------------------------------------------
    // Metrics / shutdown
    // -------------------------------------------------------------------------

    public void cleanup() { vertexGraph.cleanup(); }

    public int  getChunkCount()       { return vertexGraph.getChunkCount(); }
    public int  getLoadedChunkCount() { return vertexGraph.getLoadedChunkCount(); }
    public int  getViewDistance()     { return viewDistance; }
    public boolean isOvermindMode()   { return isOvermindMode; }

    public void shutdown() {
        logger.info("Shutting down VertexGraphManager");
        // Final save of all dirty chunks
        if (storage != null) {
            for (ChunkNode chunk : vertexGraph.getLoadedChunks()) {
                if (chunk.isDirty()) {
                    try { storage.saveChunk(chunk); } catch (IOException ignored) {}
                }
            }
        }
        cleanupExecutor.shutdown();
        try {
            if (!cleanupExecutor.awaitTermination(5, TimeUnit.SECONDS))
                cleanupExecutor.shutdownNow();
        } catch (InterruptedException e) {
            cleanupExecutor.shutdownNow();
            Thread.currentThread().interrupt();
        }
    }
}
