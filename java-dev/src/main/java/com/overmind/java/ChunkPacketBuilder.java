package com.overmind.java;

import com.overmind.api.ChunkNode;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.function.IntUnaryOperator;

/**
 * Builds Chunk Data and Update Light packets for Minecraft 1.21.4 (protocol 774).
 *
 * <h3>Packet ID</h3>
 * {@code 0x24} — "Chunk Data and Update Light" in the Play state (S→C) for protocol 774.
 *
 * <h3>Wire format (1.18+)</h3>
 * <pre>
 *   Int              chunkX
 *   Int              chunkZ
 *   NBT (Network)    heightmaps  — MOTION_BLOCKING + WORLD_SURFACE
 *   ByteArray        data        — 24 chunk sections (see section format below)
 *   VarInt           blockEntityCount (0)
 *   BitSet           skyLightMask
 *   BitSet           blockLightMask
 *   BitSet           emptySkyLightMask
 *   BitSet           emptyBlockLightMask
 *   Array&lt;2048B&gt;     skyLightArrays
 *   Array&lt;2048B&gt;     blockLightArrays
 * </pre>
 *
 * <h3>Section format</h3>
 * <pre>
 *   Short  nonAirCount
 *   PalettedContainer  blockStates  (4096 entries, YZX order)
 *   PalettedContainer  biomes       (64 entries, XZY order, 4×4×4 grid)
 * </pre>
 *
 * <h3>PalettedContainer</h3>
 * <pre>
 *   bitsPerEntry == 0  → single value: VarInt value, VarInt 0
 *   bitsPerEntry 4–8   → indirect palette: VarInt count, VarInt[] ids, VarInt dataLen, Long[] data
 *   bitsPerEntry &gt;= 15 → direct (block states only): VarInt dataLen, Long[] data
 * </pre>
 * Values are packed into longs in the non-spanning format (Minecraft 1.16+):
 * each long holds {@code floor(64/bitsPerEntry)} values; the long is padded if needed.
 */
public class ChunkPacketBuilder {

    /** Minecraft 1.21.11 (protocol 774) Chunk Data and Update Light packet ID (S→C Play). */
    public static final int CHUNK_DATA_PACKET_ID = 0x24;

    /** Number of light sections = 24 chunk sections + 1 below + 1 above = 26. */
    private static final int LIGHT_SECTION_COUNT = ChunkNode.JAVA_SECTION_COUNT + 2;

    private ChunkPacketBuilder() {}

    /**
     * Builds a Chunk Data packet for a Bedrock Edition client.
     * Currently uses the same Java 1.21.4 format; Bedrock-specific encoding can be
     * wired in here later without changing callers.
     */
    public static ByteBuf buildBedrockChunkPacket(ByteBufAllocator alloc,
                                                  int chunkX, int chunkZ,
                                                  ChunkNode chunk) {
        return buildJavaChunkPacket(alloc, chunkX, chunkZ, chunk);
    }

    // -------------------------------------------------------------------------
    // Public API
    // -------------------------------------------------------------------------

    /**
     * Builds a framed Chunk Data and Update Light packet for a Java 1.21.4 client.
     *
     * @param alloc  Netty allocator
     * @param chunkX chunk X coordinate
     * @param chunkZ chunk Z coordinate
     * @param chunk  source chunk; {@code null} produces an all-air chunk
     * @return length-prefixed packet buffer (caller must release after sending)
     */
    public static ByteBuf buildJavaChunkPacket(ByteBufAllocator alloc,
                                               int chunkX, int chunkZ,
                                               ChunkNode chunk) {
        ByteBuf payload = alloc.buffer(1 << 16);

        writeVarInt(payload, CHUNK_DATA_PACKET_ID);
        payload.writeInt(chunkX);
        payload.writeInt(chunkZ);

        // Heightmaps NBT (Network NBT: type byte present, no root name)
        writeHeightmapsNbt(payload, chunk);

        // 24 chunk sections packed as a length-prefixed byte array
        ByteBuf sectionsData = alloc.buffer(1 << 15);
        for (int sectionY = 0; sectionY < ChunkNode.JAVA_SECTION_COUNT; sectionY++) {
            writeSection(sectionsData, chunk, sectionY);
        }
        writeVarInt(payload, sectionsData.readableBytes());
        payload.writeBytes(sectionsData);
        sectionsData.release();

        // Block entities
        writeVarInt(payload, 0);

        // Light data
        writeLightData(payload);

        return framePacket(alloc, payload);
    }

    // -------------------------------------------------------------------------
    // Heightmaps NBT
    // -------------------------------------------------------------------------

    /**
     * Writes the heightmaps as a <em>nameless compound</em> (Minecraft 1.20.3+):
     * the root TAG_Compound type byte and name are omitted; only the child tags
     * and the closing TAG_End are written.
     *
     * <p>Packing: 256 values × 9 bits = 2304 bits = 36 × 64 bits.
     * Values span across long boundaries (spanning IS allowed for heightmaps,
     * unlike block-state containers).
     */
    private static void writeHeightmapsNbt(ByteBuf out, ChunkNode chunk) {
        // Nameless compound: NO 0x0A type byte, NO name. Start directly with children.

        long[] heightmap = computeHeightmap(chunk);

        // TAG_Long_Array "MOTION_BLOCKING"
        out.writeByte(0x0C);
        byte[] name1 = "MOTION_BLOCKING".getBytes(StandardCharsets.UTF_8);
        out.writeShort(name1.length);
        out.writeBytes(name1);
        out.writeInt(heightmap.length);
        for (long l : heightmap) out.writeLong(l);

        // TAG_Long_Array "WORLD_SURFACE"
        out.writeByte(0x0C);
        byte[] name2 = "WORLD_SURFACE".getBytes(StandardCharsets.UTF_8);
        out.writeShort(name2.length);
        out.writeBytes(name2);
        out.writeInt(heightmap.length);
        for (long l : heightmap) out.writeLong(l);

        out.writeByte(0x00); // TAG_End
    }

    /**
     * Computes a 256-value heightmap (one per x,z column) packed as 9-bit
     * values into 36 longs with spanning allowed.
     * Each value = Y + 1 of the highest non-air block (0 if column is all air).
     */
    private static long[] computeHeightmap(ChunkNode chunk) {
        // 256 values × 9 bits = 2304 bits = 36 × 64 bits exactly
        long[] data = new long[36];
        for (int col = 0; col < 256; col++) {
            int x = col & 15;
            int z = col >> 4;
            int height = 0;
            if (chunk != null) {
                for (int y = ChunkNode.JAVA_HEIGHT - 1; y >= 0; y--) {
                    if (chunk.getBlockForJavaClient(x, y, z) != 0) {
                        height = y + 1;
                        break;
                    }
                }
            }
            // Pack 9-bit value at bit offset col*9 (spanning across longs is allowed)
            int bitStart  = col * 9;
            int longIdx   = bitStart / 64;
            int shift     = bitStart % 64;
            data[longIdx] |= ((long) height) << shift;
            if (shift + 9 > 64 && longIdx + 1 < data.length) {
                data[longIdx + 1] |= ((long) height) >> (64 - shift);
            }
        }
        return data;
    }

    // -------------------------------------------------------------------------
    // Section encoding
    // -------------------------------------------------------------------------

    private static void writeSection(ByteBuf out, ChunkNode chunk, int sectionY) {
        int baseY = sectionY * 16;

        // Gather block state IDs (YZX order, 4096 entries)
        int[] blocks = new int[4096];
        int nonAirCount = 0;
        int idx = 0;
        for (int y = 0; y < 16; y++) {
            for (int z = 0; z < 16; z++) {
                for (int x = 0; x < 16; x++) {
                    int id = chunk != null
                            ? chunk.getBlockForJavaClient(x, baseY + y, z)
                            : 0;
                    blocks[idx++] = id;
                    if (id != 0) nonAirCount++;
                }
            }
        }

        out.writeShort(nonAirCount);

        // Block states container: indirect palette 4–8 bits, direct ≥15 bits
        writePalettedContainer(out, blocks, 4, 8);

        // Biomes container: 64 entries (4×4×4), all plains (biome 0)
        // plains ≈ ID 34 in vanilla 1.21.4; using 0 is safe if exact ID unknown
        int[] biomes = new int[64]; // all zeros = biome 0
        writePalettedContainer(out, biomes, 1, 3);
    }

    /**
     * Writes one paletted container.
     *
     * @param values          the block state / biome IDs
     * @param minBits         minimum bits for indirect palette (4 for blocks, 1 for biomes)
     * @param maxIndirectBits threshold above which direct encoding is used (8 for blocks, 3 for biomes)
     */
    private static void writePalettedContainer(ByteBuf out, int[] values,
                                               int minBits, int maxIndirectBits) {
        // All same → single-value encoding
        int first = values[0];
        boolean allSame = true;
        for (int v : values) {
            if (v != first) { allSame = false; break; }
        }
        if (allSame) {
            out.writeByte(0);
            writeVarInt(out, first);
            writeVarInt(out, 0);
            return;
        }

        // Build palette
        List<Integer>      palette      = new ArrayList<>();
        Map<Integer, Integer> paletteMap = new HashMap<>();
        for (int v : values) {
            if (!paletteMap.containsKey(v)) {
                paletteMap.put(v, palette.size());
                palette.add(v);
            }
        }

        int bitsNeeded = Math.max(minBits,
                32 - Integer.numberOfLeadingZeros(palette.size() - 1));

        if (bitsNeeded <= maxIndirectBits) {
            // Indirect palette
            out.writeByte(bitsNeeded);
            writeVarInt(out, palette.size());
            for (int id : palette) writeVarInt(out, id);
            writePackedLongs(out, values, bitsNeeded, i -> paletteMap.get(values[i]));
        } else {
            // Direct (global palette) — no palette list, 15 bits for block states
            int directBits = maxIndirectBits == 8 ? 15 : (maxIndirectBits + 1);
            out.writeByte(directBits);
            writePackedLongs(out, values, directBits, i -> values[i]);
        }
    }

    /**
     * Packs {@code values.length} entries into longs using Minecraft's non-spanning format
     * (since 1.16): each long holds {@code floor(64/bitsPerEntry)} values;
     * if a value would cross a long boundary the current long is padded and a new one starts.
     */
    private static void writePackedLongs(ByteBuf out, int[] values, int bitsPerEntry,
                                         IntUnaryOperator valueAt) {
        int valuesPerLong = 64 / bitsPerEntry;
        int dataLength    = (values.length + valuesPerLong - 1) / valuesPerLong;
        writeVarInt(out, dataLength);

        long mask        = (1L << bitsPerEntry) - 1;
        long currentLong = 0;
        int  packed      = 0;

        for (int i = 0; i < values.length; i++) {
            currentLong |= (valueAt.applyAsInt(i) & mask) << (packed * bitsPerEntry);
            packed++;
            if (packed == valuesPerLong) {
                out.writeLong(currentLong);
                currentLong = 0;
                packed = 0;
            }
        }
        if (packed > 0) {
            out.writeLong(currentLong); // last long (partially filled, rest is zero padding)
        }
    }

    // -------------------------------------------------------------------------
    // Light data
    // -------------------------------------------------------------------------

    /**
     * Writes full sky-light data (level 15 everywhere) and no block-light.
     * All 26 light sections (24 chunk sections + below + above world) are included.
     *
     * <pre>
     *   skyLightMask        = all 26 bits set
     *   blockLightMask      = 0 (no block-light sources)
     *   emptySkyLightMask   = 0 (all sections have sky-light arrays)
     *   emptyBlockLightMask = all 26 bits set (all sections are dark block-light)
     *   26 sky-light arrays  of 2048 bytes = 0xFF each (nibble = 15)
     *   0 block-light arrays
     * </pre>
     */
    private static void writeLightData(ByteBuf out) {
        long allLit = (1L << LIGHT_SECTION_COUNT) - 1; // bits 0-25

        writeBitSet(out, allLit); // Sky Light Mask
        writeBitSet(out, 0L);     // Block Light Mask
        writeBitSet(out, 0L);     // Empty Sky Light Mask
        writeBitSet(out, allLit); // Empty Block Light Mask

        // Sky Light Arrays: 26 × 2048 bytes of 0xFF
        writeVarInt(out, LIGHT_SECTION_COUNT);
        byte[] fullLight = new byte[2048];
        Arrays.fill(fullLight, (byte) 0xFF);
        for (int i = 0; i < LIGHT_SECTION_COUNT; i++) {
            writeVarInt(out, 2048);
            out.writeBytes(fullLight);
        }

        // Block Light Arrays: 0
        writeVarInt(out, 0);
    }

    /** Writes a Minecraft BitSet: VarInt(1) + one Long. */
    private static void writeBitSet(ByteBuf out, long bits) {
        writeVarInt(out, 1);
        out.writeLong(bits);
    }

    // -------------------------------------------------------------------------
    // Helpers
    // -------------------------------------------------------------------------

    private static ByteBuf framePacket(ByteBufAllocator alloc, ByteBuf payload) {
        ByteBuf framed = alloc.buffer();
        writeVarInt(framed, payload.readableBytes());
        framed.writeBytes(payload);
        payload.release();
        return framed;
    }

    private static void writeVarInt(ByteBuf buf, int value) {
        while (true) {
            if ((value & 0xFFFFFF80) == 0) { buf.writeByte(value); return; }
            buf.writeByte((value & 0x7F) | 0x80);
            value >>>= 7;
        }
    }
}
