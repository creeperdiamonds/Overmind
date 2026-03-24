package com.overmind.api;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.HashMap;
import java.util.Map;

/**
 * Maps Java Edition 1.21.4 global block-state IDs to Bedrock Edition 1.21 runtime block IDs.
 *
 * <p>Bedrock runtime IDs are version-specific. The constants here are best-known values for
 * Bedrock 1.21.x (legacy numeric IDs). For authoritative mappings, call {@link #register} with
 * data extracted from a Bedrock server's {@code resource_packs/vanilla/blocks.json} or the
 * runtime ID table from a proxy such as GeyserMC.
 *
 * <p>Unknown Java state IDs fall back to {@link #setFallback} (default: air = 0) and a warning
 * is logged once per unknown ID.
 *
 * <h3>Usage in chunk pipeline</h3>
 * When building a Bedrock chunk packet, iterate each block-state ID from the Java chunk and
 * call {@code BlockStateRegistry.toBedrock(id)} before writing the value.
 */
public final class BlockStateRegistry {
    private static final Logger logger = LoggerFactory.getLogger(BlockStateRegistry.class);

    // ── Bedrock 1.21.x runtime IDs (best-known estimates) ───────────────────
    public static final int BEDROCK_AIR          = 0;
    public static final int BEDROCK_STONE        = 1;
    public static final int BEDROCK_GRASS        = 2;
    public static final int BEDROCK_DIRT         = 3;
    public static final int BEDROCK_COBBLESTONE  = 4;
    public static final int BEDROCK_BEDROCK      = 7;
    public static final int BEDROCK_SAND         = 12;
    public static final int BEDROCK_GRAVEL       = 13;
    public static final int BEDROCK_GOLD_ORE     = 14;
    public static final int BEDROCK_IRON_ORE     = 15;
    public static final int BEDROCK_COAL_ORE     = 16;
    public static final int BEDROCK_LOG          = 17;
    public static final int BEDROCK_LEAVES       = 18;
    public static final int BEDROCK_GLASS        = 20;
    public static final int BEDROCK_OBSIDIAN     = 49;
    public static final int BEDROCK_NETHERRACK   = 87;
    public static final int BEDROCK_SOUL_SAND    = 88;
    public static final int BEDROCK_GLOWSTONE    = 89;
    public static final int BEDROCK_END_STONE    = 121;
    public static final int BEDROCK_WATER        = 8;   // flowing; still = 9

    private static final Map<Integer, Integer> JAVA_TO_BEDROCK = new HashMap<>(512);
    private static volatile int fallbackId = BEDROCK_AIR;

    static {
        // Core terrain blocks used by our world generators
        map(BlockStates.AIR,         BEDROCK_AIR);
        map(BlockStates.STONE,       BEDROCK_STONE);
        map(BlockStates.GRASS_BLOCK, BEDROCK_GRASS);
        map(BlockStates.DIRT,        BEDROCK_DIRT);
        map(BlockStates.BEDROCK,     BEDROCK_BEDROCK);
        map(BlockStates.SAND,        BEDROCK_SAND);
        map(BlockStates.GRAVEL,      BEDROCK_GRAVEL);
        map(BlockStates.NETHERRACK,  BEDROCK_NETHERRACK);
        map(BlockStates.END_STONE,   BEDROCK_END_STONE);
        map(BlockStates.OBSIDIAN,    BEDROCK_OBSIDIAN);
    }

    private BlockStateRegistry() {}

    private static void map(int javaId, int bedrockId) {
        JAVA_TO_BEDROCK.put(javaId, bedrockId);
    }

    // ── Public API ───────────────────────────────────────────────────────────

    /**
     * Translates a Java global block-state ID to a Bedrock runtime block ID.
     *
     * <p>Unknown IDs are logged as warnings the first time they appear, then cached
     * as the fallback value to suppress repeated warnings.
     */
    public static int toBedrock(int javaStateId) {
        Integer result = JAVA_TO_BEDROCK.get(javaStateId);
        if (result == null) {
            logger.warn("Unmapped Java block state ID {} — rendering as fallback block {}",
                    javaStateId, fallbackId);
            JAVA_TO_BEDROCK.put(javaStateId, fallbackId); // cache to suppress duplicates
            return fallbackId;
        }
        return result;
    }

    /**
     * Adds or overrides a single mapping entry.
     * Call at startup when loading mappings from a Bedrock palette file or proxy table.
     */
    public static void register(int javaStateId, int bedrockRuntimeId) {
        JAVA_TO_BEDROCK.put(javaStateId, bedrockRuntimeId);
    }

    /**
     * Bulk-registers a complete Java→Bedrock mapping table (e.g. loaded from a palette file).
     * Existing entries are overwritten.
     */
    public static void registerAll(Map<Integer, Integer> mappings) {
        JAVA_TO_BEDROCK.putAll(mappings);
        logger.info("BlockStateRegistry: loaded {} mappings ({} total)",
                mappings.size(), JAVA_TO_BEDROCK.size());
    }

    /**
     * Sets the Bedrock runtime ID used when no mapping exists for a Java block state.
     * Default is {@link #BEDROCK_AIR} (0).
     */
    public static void setFallback(int bedrockRuntimeId) {
        fallbackId = bedrockRuntimeId;
    }

    /** Number of registered mappings (including auto-cached fallback entries). */
    public static int size() {
        return JAVA_TO_BEDROCK.size();
    }
}
