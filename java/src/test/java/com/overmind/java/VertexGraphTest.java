package com.overmind.java;

import com.overmind.api.ChunkNode;
import com.overmind.api.VertexGraphManager;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.util.Set;

import static org.junit.jupiter.api.Assertions.*;

public class VertexGraphTest {
    private VertexGraphManager vertexGraphManager;

    @BeforeEach
    void setUp() {
        vertexGraphManager = new VertexGraphManager(8, true); // Overmind mode
    }

    @Test
    void test383Limit() {
        ChunkNode chunk = vertexGraphManager.getOrCreateChunk(0, 0);

        // Block at y=383 must be cleared by ensure383Limit
        chunk.setBlock(0, 383, 0, (short) 1);
        chunk.ensure383Limit();
        assertEquals(0, chunk.getBlock(0, 383, 0));

        // setBlock at y=384 is a no-op (out of array bounds guard)
        vertexGraphManager.setBlock(0, 0, 0, 384, 0, (short) 1);
        assertEquals(0, vertexGraphManager.getBlock(0, 0, 0, 384, 0));

        // Normal block at y=382 must persist
        vertexGraphManager.setBlock(0, 0, 0, 382, 0, (short) 1);
        assertEquals(1, vertexGraphManager.getBlock(0, 0, 0, 382, 0));
    }

    @Test
    void testChunkLoading() {
        vertexGraphManager.loadChunk(0, 0);
        assertTrue(vertexGraphManager.getChunk(0, 0).isLoaded());
        assertTrue(vertexGraphManager.getChunk(0, 0).getLoadLevel() >= 1);

        vertexGraphManager.loadArea(0, 0, 2);
        assertTrue(vertexGraphManager.getChunk(1, 1).isLoaded());
        assertTrue(vertexGraphManager.getChunk(-1, -1).isLoaded());
    }

    @Test
    void testOvermindMode() {
        assertTrue(vertexGraphManager.isOvermindMode());

        ChunkNode chunk = vertexGraphManager.getOrCreateChunk(0, 0);
        chunk.setBlock(0, 383, 0, (short) 1);
        vertexGraphManager.ensure383Limit();
        assertEquals(0, chunk.getBlock(0, 383, 0));
    }

    @Test
    void testChunkCount() {
        assertEquals(0, vertexGraphManager.getChunkCount());
        assertEquals(0, vertexGraphManager.getLoadedChunkCount());

        vertexGraphManager.loadChunk(0, 0);
        assertTrue(vertexGraphManager.getChunkCount() >= 1);
        assertTrue(vertexGraphManager.getLoadedChunkCount() >= 1);
    }

    // -------------------------------------------------------------------------
    // Phase B: height-boundary tests
    // -------------------------------------------------------------------------

    @Test
    void testJavaClientHeightBoundary() {
        ChunkNode chunk = vertexGraphManager.getOrCreateChunk(0, 0);

        // Place a real block at y=382 — Java client should see it
        chunk.setBlock(5, 382, 5, (short) 42);
        assertEquals(42, chunk.getBlockForJavaClient(5, 382, 5),
                "Java client must see real block at y=382");

        // y=383 must always be reported as air for Java clients (fake injected layer)
        chunk.setBlock(5, 383, 5, (short) 7);
        assertEquals(0, chunk.getBlockForJavaClient(5, 383, 5),
                "Java client must see air at y=383 (fake-air injection)");
    }

    @Test
    void testBedrockClientHeightBoundary() {
        ChunkNode chunk = vertexGraphManager.getOrCreateChunk(0, 0);

        // Place a real block at y=382 — Bedrock client should see it
        chunk.setBlock(3, 382, 3, (short) 99);
        assertEquals(99, chunk.getBlockForBedrockClient(3, 382, 3),
                "Bedrock client must see real block at y=382");

        // y=383 must always be air for Bedrock clients (stripped layer)
        chunk.setBlock(3, 383, 3, (short) 7);
        assertEquals(0, chunk.getBlockForBedrockClient(3, 383, 3),
                "Bedrock client must see air at y=383 (stripped layer)");
    }

    @Test
    void testJavaAndBedrockDifferOnlyAtY383() {
        ChunkNode chunk = vertexGraphManager.getOrCreateChunk(0, 0);
        chunk.setBlock(0, 383, 0, (short) 5);

        // Both return 0 at y=383 — Java because it's the fake-air injection,
        // Bedrock because it strips that layer. The storage value (5) must NOT leak.
        assertEquals(0, chunk.getBlockForJavaClient(0, 383, 0),
                "Java must not expose raw y=383 storage value");
        assertEquals(0, chunk.getBlockForBedrockClient(0, 383, 0),
                "Bedrock must not expose raw y=383 storage value");

        // Below the boundary both clients agree
        chunk.setBlock(0, 100, 0, (short) 3);
        assertEquals(chunk.getBlockForJavaClient(0, 100, 0),
                     chunk.getBlockForBedrockClient(0, 100, 0),
                "Java and Bedrock must return identical blocks below y=383");
    }

    @Test
    void testSectionConstants() {
        assertEquals(384, ChunkNode.JAVA_HEIGHT);
        assertEquals(383, ChunkNode.BEDROCK_HEIGHT);
        assertEquals(24, ChunkNode.JAVA_SECTION_COUNT);
        assertEquals(24, ChunkNode.BEDROCK_SECTION_COUNT);
    }

    // -------------------------------------------------------------------------
    // Phase B: 10×10 stress test
    // -------------------------------------------------------------------------

    @Test
    void testStressLoad10x10() {
        // Load a 10×10 grid
        for (int x = 0; x < 10; x++) {
            for (int z = 0; z < 10; z++) {
                vertexGraphManager.loadChunk(x, z);
            }
        }

        // All 100 chunks must be loaded
        for (int x = 0; x < 10; x++) {
            for (int z = 0; z < 10; z++) {
                ChunkNode chunk = vertexGraphManager.getChunk(x, z);
                assertNotNull(chunk, "Chunk (" + x + ", " + z + ") must not be null");
                assertTrue(chunk.isLoaded(), "Chunk (" + x + ", " + z + ") must be loaded");
            }
        }

        // Interior chunks (1..8 × 1..8) must all have non-null, non-empty neighbor sets
        for (int x = 1; x <= 8; x++) {
            for (int z = 1; z <= 8; z++) {
                Set<ChunkNode> neighbors = vertexGraphManager.getNeighbors(x, z);
                assertNotNull(neighbors, "Neighbors of (" + x + ", " + z + ") must not be null");
                assertFalse(neighbors.isEmpty(),
                        "Neighbors of interior chunk (" + x + ", " + z + ") must not be empty");
                // Interior chunks have 8 adjacent cells (Moore neighbourhood), all loaded
                assertEquals(8, neighbors.size(),
                        "Interior chunk (" + x + ", " + z + ") must have exactly 8 neighbours");
            }
        }

        assertTrue(vertexGraphManager.getLoadedChunkCount() >= 100,
                "At least 100 chunks must be loaded after 10×10 area load");
    }
}
