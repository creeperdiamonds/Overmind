package com.overmind.api;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.io.Reader;
import java.io.StringReader;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;

/**
 * Parses Bedrock Edition {@code .geo.json} model files (format_version 1.8–1.21).
 *
 * <h3>Expected file structure</h3>
 * <pre>{@code
 * {
 *   "format_version": "1.12.0",
 *   "minecraft:geometry": [
 *     {
 *       "description": { "identifier": "geometry.example", ... },
 *       "bones": [
 *         { "name": "body", "parent": "root", "pivot": [0,12,0], "rotation": [0,0,0] },
 *         ...
 *       ]
 *     }
 *   ]
 * }
 * }</pre>
 *
 * <p>Malformed files log a warning and return an empty list rather than throwing.
 */
public final class GeoModelParser {
    private static final Logger logger = LoggerFactory.getLogger(GeoModelParser.class);

    private GeoModelParser() {}

    /**
     * Parses all bones from a {@code .geo.json} file on disk.
     *
     * @return bones in definition order; empty list if the file is unreadable or malformed
     */
    public static List<GeoModelBone> parse(Path path) {
        try (Reader reader = Files.newBufferedReader(path)) {
            return parseReader(reader, path.toString());
        } catch (IOException e) {
            logger.warn("GeoModelParser: could not read '{}': {}", path, e.getMessage());
            return Collections.emptyList();
        }
    }

    /**
     * Parses all bones from a JSON string (useful for in-memory / resource-pack workflows).
     *
     * @param json       raw geo.json content
     * @param sourceName label used in log messages
     * @return bones in definition order; empty list on parse error
     */
    public static List<GeoModelBone> parseString(String json, String sourceName) {
        try {
            return parseReader(new StringReader(json), sourceName);
        } catch (Exception e) {
            logger.warn("GeoModelParser: failed to parse '{}': {}", sourceName, e.getMessage());
            return Collections.emptyList();
        }
    }

    // ── Internal ─────────────────────────────────────────────────────────────

    private static List<GeoModelBone> parseReader(Reader reader, String source) throws IOException {
        JsonElement root;
        try {
            root = JsonParser.parseReader(reader);
        } catch (Exception e) {
            logger.warn("GeoModelParser: JSON syntax error in '{}': {}", source, e.getMessage());
            return Collections.emptyList();
        }

        if (!root.isJsonObject()) {
            logger.warn("GeoModelParser: root is not a JSON object in '{}'", source);
            return Collections.emptyList();
        }

        JsonObject rootObj = root.getAsJsonObject();
        JsonElement geoEl  = rootObj.get("minecraft:geometry");

        if (geoEl == null) {
            // Older format_version 1.8 puts geometry definitions as top-level keys
            logger.debug("GeoModelParser: no 'minecraft:geometry' array, trying legacy format for '{}'", source);
            return parseLegacy(rootObj, source);
        }

        List<GeoModelBone> allBones = new ArrayList<>();
        for (JsonElement entry : geoEl.getAsJsonArray()) {
            JsonObject geoDef = entry.getAsJsonObject();
            JsonElement bonesEl = geoDef.get("bones");
            if (bonesEl == null || !bonesEl.isJsonArray()) continue;
            for (JsonElement boneEl : bonesEl.getAsJsonArray()) {
                GeoModelBone bone = parseBone(boneEl.getAsJsonObject());
                if (bone != null) allBones.add(bone);
            }
        }
        logger.debug("GeoModelParser: loaded {} bones from '{}'", allBones.size(), source);
        return allBones;
    }

    /** Handles the older 1.8 format where geometry definitions are top-level keys like {@code geometry.example}. */
    private static List<GeoModelBone> parseLegacy(JsonObject root, String source) {
        List<GeoModelBone> bones = new ArrayList<>();
        for (Map.Entry<String, JsonElement> entry : root.entrySet()) {
            if (!entry.getKey().startsWith("geometry.")) continue;
            JsonElement val = entry.getValue();
            if (!val.isJsonObject()) continue;
            JsonElement bonesEl = val.getAsJsonObject().get("bones");
            if (bonesEl == null || !bonesEl.isJsonArray()) continue;
            for (JsonElement boneEl : bonesEl.getAsJsonArray()) {
                GeoModelBone bone = parseBone(boneEl.getAsJsonObject());
                if (bone != null) bones.add(bone);
            }
        }
        logger.debug("GeoModelParser: legacy format loaded {} bones from '{}'", bones.size(), source);
        return bones;
    }

    private static GeoModelBone parseBone(JsonObject obj) {
        try {
            String name   = obj.has("name")   ? obj.get("name").getAsString()   : "unknown";
            String parent = obj.has("parent") ? obj.get("parent").getAsString() : null;

            float[] pivot = parseVec3(obj, "pivot");
            float[] rot   = parseVec3(obj, "rotation");

            return new GeoModelBone(name, parent,
                    pivot[0], pivot[1], pivot[2],
                    rot[0],   rot[1],   rot[2]);
        } catch (Exception e) {
            logger.warn("GeoModelParser: skipping malformed bone entry: {}", e.getMessage());
            return null;
        }
    }

    private static float[] parseVec3(JsonObject obj, String key) {
        if (!obj.has(key)) return new float[]{0f, 0f, 0f};
        JsonArray arr = obj.get(key).getAsJsonArray();
        return new float[]{
            arr.size() > 0 ? arr.get(0).getAsFloat() : 0f,
            arr.size() > 1 ? arr.get(1).getAsFloat() : 0f,
            arr.size() > 2 ? arr.get(2).getAsFloat() : 0f
        };
    }
}
