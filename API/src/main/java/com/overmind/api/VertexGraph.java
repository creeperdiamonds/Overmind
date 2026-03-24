package com.overmind.api;

import java.util.*;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.locks.ReadWriteLock;
import java.util.concurrent.locks.ReentrantReadWriteLock;

public class VertexGraph {
    private final Map<String, ChunkNode> chunks;
    private final Map<String, Set<String>> neighbors;
    private final ReadWriteLock lock;
    private final int viewDistance;
    
    public VertexGraph(int viewDistance) {
        this.chunks = new ConcurrentHashMap<>();
        this.neighbors = new ConcurrentHashMap<>();
        this.lock = new ReentrantReadWriteLock();
        this.viewDistance = viewDistance;
    }
    
    public ChunkNode getChunk(int chunkX, int chunkZ) {
        String key = getChunkKey(chunkX, chunkZ);
        lock.readLock().lock();
        try {
            return chunks.get(key);
        } finally {
            lock.readLock().unlock();
        }
    }
    
    /**
     * Inserts a pre-built (e.g. generated or loaded) chunk into the graph,
     * wires up neighbor edges, and increments its load level.
     */
    public void putChunk(ChunkNode chunk) {
        String key = getChunkKey(chunk.getChunkX(), chunk.getChunkZ());
        lock.writeLock().lock();
        try {
            chunks.put(key, chunk);
            updateNeighbors(chunk.getChunkX(), chunk.getChunkZ());
            chunk.markLoaded();
            chunk.incrementLoadLevel();
        } finally {
            lock.writeLock().unlock();
        }
    }

    public ChunkNode getOrCreateChunk(int chunkX, int chunkZ) {
        String key = getChunkKey(chunkX, chunkZ);
        lock.writeLock().lock();
        try {
            return chunks.computeIfAbsent(key, k -> {
                ChunkNode chunk = new ChunkNode(chunkX, chunkZ);
                updateNeighbors(chunkX, chunkZ);
                return chunk;
            });
        } finally {
            lock.writeLock().unlock();
        }
    }
    
    public void loadChunk(int chunkX, int chunkZ) {
        ChunkNode chunk = getOrCreateChunk(chunkX, chunkZ);
        chunk.markLoaded();
        chunk.incrementLoadLevel();
        
        for (int dx = -viewDistance; dx <= viewDistance; dx++) {
            for (int dz = -viewDistance; dz <= viewDistance; dz++) {
                int neighborX = chunkX + dx;
                int neighborZ = chunkZ + dz;
                ChunkNode neighbor = getOrCreateChunk(neighborX, neighborZ);
                neighbor.incrementLoadLevel();
            }
        }
    }
    
    public void unloadChunk(int chunkX, int chunkZ) {
        ChunkNode chunk = getChunk(chunkX, chunkZ);
        if (chunk != null) {
            chunk.markUnloaded();
            chunk.decrementLoadLevel();
            
            for (int dx = -viewDistance; dx <= viewDistance; dx++) {
                for (int dz = -viewDistance; dz <= viewDistance; dz++) {
                    int neighborX = chunkX + dx;
                    int neighborZ = chunkZ + dz;
                    ChunkNode neighbor = getChunk(neighborX, neighborZ);
                    if (neighbor != null) {
                        neighbor.decrementLoadLevel();
                    }
                }
            }
        }
    }
    
    public Set<ChunkNode> getNeighbors(int chunkX, int chunkZ) {
        String key = getChunkKey(chunkX, chunkZ);
        lock.readLock().lock();
        try {
            Set<String> neighborKeys = neighbors.get(key);
            if (neighborKeys == null) {
                return Collections.emptySet();
            }
            
            Set<ChunkNode> result = new HashSet<>();
            for (String neighborKey : neighborKeys) {
                ChunkNode neighbor = chunks.get(neighborKey);
                if (neighbor != null) {
                    result.add(neighbor);
                }
            }
            return result;
        } finally {
            lock.readLock().unlock();
        }
    }
    
    public List<ChunkNode> getLoadedChunks() {
        lock.readLock().lock();
        try {
            List<ChunkNode> loaded = new ArrayList<>();
            for (ChunkNode chunk : chunks.values()) {
                if (chunk.isLoaded()) {
                    loaded.add(chunk);
                }
            }
            return loaded;
        } finally {
            lock.readLock().unlock();
        }
    }
    
    public void ensure383Limit() {
        lock.readLock().lock();
        try {
            for (ChunkNode chunk : chunks.values()) {
                chunk.ensure383Limit();
            }
        } finally {
            lock.readLock().unlock();
        }
    }
    
    public void cleanup() {
        lock.writeLock().lock();
        try {
            Iterator<Map.Entry<String, ChunkNode>> iterator = chunks.entrySet().iterator();
            while (iterator.hasNext()) {
                Map.Entry<String, ChunkNode> entry = iterator.next();
                ChunkNode chunk = entry.getValue();
                if (!chunk.isLoaded() && chunk.getLoadLevel() <= 0) {
                    iterator.remove();
                    neighbors.remove(entry.getKey());
                }
            }
        } finally {
            lock.writeLock().unlock();
        }
    }
    
    private void updateNeighbors(int chunkX, int chunkZ) {
        String key = getChunkKey(chunkX, chunkZ);
        Set<String> neighborKeys = new HashSet<>();
        
        for (int dx = -1; dx <= 1; dx++) {
            for (int dz = -1; dz <= 1; dz++) {
                if (dx == 0 && dz == 0) continue;
                
                int neighborX = chunkX + dx;
                int neighborZ = chunkZ + dz;
                String neighborKey = getChunkKey(neighborX, neighborZ);
                neighborKeys.add(neighborKey);
                
                neighbors.computeIfAbsent(neighborKey, k -> new HashSet<>()).add(key);
            }
        }
        
        neighbors.put(key, neighborKeys);
    }
    
    private String getChunkKey(int chunkX, int chunkZ) {
        return chunkX + "," + chunkZ;
    }
    
    public int getChunkCount() {
        lock.readLock().lock();
        try {
            return chunks.size();
        } finally {
            lock.readLock().unlock();
        }
    }
    
    public int getLoadedChunkCount() {
        lock.readLock().lock();
        try {
            int count = 0;
            for (ChunkNode chunk : chunks.values()) {
                if (chunk.isLoaded()) {
                    count++;
                }
            }
            return count;
        } finally {
            lock.readLock().unlock();
        }
    }
}
