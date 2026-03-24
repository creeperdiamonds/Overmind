package com.overmind.api;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.*;
import java.nio.file.*;
import java.util.zip.*;

/**
 * Persists generated {@link ChunkNode} data to disk under
 * {@code server/java/<dimension>/c.<x>.<z>.bin}.
 *
 * <p>Each file is a gzip-compressed stream of 384×16×16 signed shorts
 * (block-state IDs) in YZX order.
 */
public class WorldStorage {
    private static final Logger logger = LoggerFactory.getLogger(WorldStorage.class);

    private final Path chunkDir;

    /**
     * @param dimensionId short ID such as {@code "overworld"}, {@code "nether"}, {@code "end"}
     * @param serverRoot  root storage path (e.g. {@code Paths.get("server", "java")})
     */
    public WorldStorage(String dimensionId, Path serverRoot) throws IOException {
        this.chunkDir = serverRoot.resolve(dimensionId);
        Files.createDirectories(chunkDir);
        logger.info("WorldStorage ready at {}", chunkDir.toAbsolutePath());
    }

    /** Returns true if a saved file exists for the given coordinates. */
    public boolean hasChunk(int chunkX, int chunkZ) {
        return Files.exists(path(chunkX, chunkZ));
    }

    /**
     * Loads a chunk from disk.
     *
     * @throws IOException if the file cannot be read or is corrupt
     */
    public ChunkNode loadChunk(int chunkX, int chunkZ) throws IOException {
        ChunkNode chunk = new ChunkNode(chunkX, chunkZ);
        short[][][] blocks = chunk.getBlocks();
        try (DataInputStream dis = new DataInputStream(
                new GZIPInputStream(Files.newInputStream(path(chunkX, chunkZ))))) {
            for (int y = 0; y < ChunkNode.JAVA_HEIGHT; y++)
                for (int z = 0; z < 16; z++)
                    for (int x = 0; x < 16; x++)
                        blocks[x][y][z] = dis.readShort();
        }
        chunk.markLoaded();
        logger.debug("Loaded chunk ({}, {}) from disk", chunkX, chunkZ);
        return chunk;
    }

    /**
     * Saves a chunk to disk and marks it clean.
     *
     * @throws IOException if the file cannot be written
     */
    public void saveChunk(ChunkNode chunk) throws IOException {
        short[][][] blocks = chunk.getBlocks();
        try (DataOutputStream dos = new DataOutputStream(
                new GZIPOutputStream(Files.newOutputStream(path(chunk.getChunkX(), chunk.getChunkZ()))))) {
            for (int y = 0; y < ChunkNode.JAVA_HEIGHT; y++)
                for (int z = 0; z < 16; z++)
                    for (int x = 0; x < 16; x++)
                        dos.writeShort(blocks[x][y][z]);
        }
        chunk.markClean();
        logger.debug("Saved chunk ({}, {})", chunk.getChunkX(), chunk.getChunkZ());
    }

    private Path path(int x, int z) {
        return chunkDir.resolve("c." + x + "." + z + ".bin");
    }
}
