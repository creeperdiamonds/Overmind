package com.overmind.api;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import org.joml.Matrix4f;
import org.joml.Quaternionf;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.io.Reader;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Collections;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Manages Bedrock animation playback and converts keyframe data to per-bone
 * transform matrices suitable for Java Item Display entities.
 *
 * <h3>Animation JSON structure</h3>
 * <pre>{@code
 * {
 *   "format_version": "1.8.0",
 *   "animations": {
 *     "animation.entity.walk": {
 *       "animation_length": 1.0,
 *       "loop": true,
 *       "bones": {
 *         "body": {
 *           "rotation": { "0.0": [0, "math.sin(q.anim_time * 38.17) * 20", 0] },
 *           "position": { "0.0": [0, 0, 0] },
 *           "scale":    { "0.0": [1, 1, 1] }
 *         }
 *       }
 *     }
 *   }
 * }
 * }</pre>
 *
 * <p>Keyframe values may be numeric literals or Molang expression strings.
 * Keyframes are interpolated linearly between timestamps.
 *
 * <h3>Usage</h3>
 * <pre>{@code
 * AnimationController ctrl = new AnimationController(bones);
 * ctrl.loadAnimations(Paths.get("entity/walk.animation.json"));
 * ctrl.play("animation.entity.walk");
 *
 * // Each server tick:
 * ctrl.tick(0.05); // 50ms ≈ 1 game tick
 * Map<String, Matrix4f> matrices = ctrl.getBoneMatrices();
 * // Send matrices to Java clients via ItemDisplayPacketBuilder
 * }</pre>
 */
public class AnimationController {
    private static final Logger logger = LoggerFactory.getLogger(AnimationController.class);

    /** Per-bone key: three Molang expressions for X/Y/Z. */
    private static final class Vec3Track {
        final MolangExpression x, y, z;
        Vec3Track(MolangExpression x, MolangExpression y, MolangExpression z) {
            this.x = x; this.y = y; this.z = z;
        }
    }

    /** One keyframe timestamp → Vec3 values for rotation/position/scale. */
    private static final class Keyframe {
        final double time;
        final Vec3Track value;
        Keyframe(double time, Vec3Track value) { this.time = time; this.value = value; }
    }

    /** Per-bone animation channels. */
    private static final class BoneTrack {
        final List<Keyframe> rotation  = new java.util.ArrayList<>();
        final List<Keyframe> position  = new java.util.ArrayList<>();
        final List<Keyframe> scale     = new java.util.ArrayList<>();
    }

    /** One loaded animation clip. */
    private static final class AnimClip {
        final double length;
        final boolean loop;
        final Map<String, BoneTrack> bones;
        AnimClip(double length, boolean loop, Map<String, BoneTrack> bones) {
            this.length = length; this.loop = loop; this.bones = bones;
        }
    }

    // ── State ────────────────────────────────────────────────────────────────

    private final List<GeoModelBone> modelBones;
    private final Map<String, AnimClip> clips = new HashMap<>();

    private String currentAnim    = null;
    private double animTime       = 0;
    private boolean playing       = false;

    public AnimationController(List<GeoModelBone> modelBones) {
        this.modelBones = modelBones;
    }

    // ── Animation loading ────────────────────────────────────────────────────

    /**
     * Loads animation clips from a Bedrock {@code .animation.json} file.
     * Existing clips with the same name are replaced.
     */
    public void loadAnimations(Path path) {
        try (Reader reader = Files.newBufferedReader(path)) {
            JsonObject root = JsonParser.parseReader(reader).getAsJsonObject();
            JsonObject anims = root.getAsJsonObject("animations");
            if (anims == null) return;
            for (Map.Entry<String, JsonElement> entry : anims.entrySet()) {
                AnimClip clip = parseClip(entry.getKey(), entry.getValue().getAsJsonObject());
                if (clip != null) clips.put(entry.getKey(), clip);
            }
            logger.info("AnimationController: loaded {} clips from '{}'", clips.size(), path);
        } catch (IOException e) {
            logger.warn("AnimationController: could not read '{}': {}", path, e.getMessage());
        } catch (Exception e) {
            logger.warn("AnimationController: failed to parse animations from '{}': {}", path, e.getMessage());
        }
    }

    private AnimClip parseClip(String name, JsonObject obj) {
        double length = obj.has("animation_length") ? obj.get("animation_length").getAsDouble() : 1.0;
        boolean loop  = obj.has("loop") && obj.get("loop").getAsBoolean();
        Map<String, BoneTrack> boneMap = new LinkedHashMap<>();

        JsonObject bonesEl = obj.has("bones") ? obj.getAsJsonObject("bones") : null;
        if (bonesEl != null) {
            for (Map.Entry<String, JsonElement> boneEntry : bonesEl.entrySet()) {
                String boneName = boneEntry.getKey();
                JsonObject boneObj = boneEntry.getValue().getAsJsonObject();
                BoneTrack track = new BoneTrack();
                parseChannel(boneObj, "rotation", track.rotation);
                parseChannel(boneObj, "position", track.position);
                parseChannel(boneObj, "scale",    track.scale);
                boneMap.put(boneName, track);
            }
        }
        return new AnimClip(length, loop, boneMap);
    }

    /** Parses keyframe entries for one channel (rotation/position/scale). */
    private void parseChannel(JsonObject boneObj, String key, List<Keyframe> out) {
        JsonElement el = boneObj.get(key);
        if (el == null) return;

        if (el.isJsonArray()) {
            // Constant value (no keyframes), treat as t=0
            Vec3Track v = parseVec3El(el.getAsJsonArray());
            out.add(new Keyframe(0, v));
            return;
        }

        if (el.isJsonObject()) {
            JsonObject kfObj = el.getAsJsonObject();
            for (Map.Entry<String, JsonElement> kf : kfObj.entrySet()) {
                try {
                    double t = Double.parseDouble(kf.getKey());
                    Vec3Track v;
                    if (kf.getValue().isJsonArray()) {
                        v = parseVec3El(kf.getValue().getAsJsonArray());
                    } else {
                        // Keyframe may be an object with "pre"/"post" lerp targets
                        JsonObject kfDetails = kf.getValue().getAsJsonObject();
                        JsonElement pre = kfDetails.has("pre") ? kfDetails.get("pre")
                                        : kfDetails.has("post") ? kfDetails.get("post") : null;
                        v = (pre != null && pre.isJsonArray())
                            ? parseVec3El(pre.getAsJsonArray())
                            : new Vec3Track(MolangExpression.ZERO, MolangExpression.ZERO, MolangExpression.ZERO);
                    }
                    out.add(new Keyframe(t, v));
                } catch (NumberFormatException ignored) {}
            }
            out.sort((a, b) -> Double.compare(a.time, b.time));
        }
    }

    private Vec3Track parseVec3El(com.google.gson.JsonArray arr) {
        MolangExpression x = arr.size() > 0 ? parseMolang(arr.get(0)) : MolangExpression.ZERO;
        MolangExpression y = arr.size() > 1 ? parseMolang(arr.get(1)) : MolangExpression.ZERO;
        MolangExpression z = arr.size() > 2 ? parseMolang(arr.get(2)) : MolangExpression.ZERO;
        return new Vec3Track(x, y, z);
    }

    private MolangExpression parseMolang(JsonElement el) {
        if (el.isJsonPrimitive()) {
            if (el.getAsJsonPrimitive().isNumber()) {
                return MolangExpression.parse(String.valueOf(el.getAsDouble()));
            }
            return MolangExpression.parse(el.getAsString());
        }
        return MolangExpression.ZERO;
    }

    // ── Playback ─────────────────────────────────────────────────────────────

    /** Starts playing the named animation. No-op if the clip is not loaded. */
    public void play(String animationName) {
        if (!clips.containsKey(animationName)) {
            logger.warn("AnimationController: animation '{}' not loaded", animationName);
            return;
        }
        currentAnim = animationName;
        animTime    = 0;
        playing     = true;
    }

    /** Stops playback and resets the animation time. */
    public void stop() {
        playing  = false;
        animTime = 0;
    }

    /**
     * Advances the animation by {@code deltaSeconds} and recomputes bone matrices.
     * Call once per server tick (50ms = 0.05s for 20 TPS).
     */
    public void tick(double deltaSeconds) {
        if (!playing || currentAnim == null) return;
        AnimClip clip = clips.get(currentAnim);
        if (clip == null) return;

        animTime += deltaSeconds;
        if (clip.loop && clip.length > 0) {
            animTime %= clip.length;
        } else if (animTime >= clip.length) {
            animTime = clip.length;
            playing  = false;
        }
    }

    /**
     * Returns the current world-space transform matrix for each bone.
     * If no animation is playing, returns the bind-pose matrix.
     *
     * <p>The returned map is freshly computed on each call; cache the result per tick.
     *
     * @return map of bone name → world-space 4×4 transform matrix (JOML {@link Matrix4f})
     */
    public Map<String, Matrix4f> getBoneMatrices() {
        Map<String, Matrix4f> localMatrices = new LinkedHashMap<>();

        AnimClip clip = (currentAnim != null) ? clips.get(currentAnim) : null;

        // First pass: compute local bone matrices
        for (GeoModelBone bone : modelBones) {
            Matrix4f mat = computeLocalMatrix(bone, clip, animTime);
            localMatrices.put(bone.name, mat);
        }

        // Second pass: multiply by parent matrices to get world-space
        Map<String, Matrix4f> worldMatrices = new LinkedHashMap<>();
        Map<String, String> parentMap = new HashMap<>();
        for (GeoModelBone bone : modelBones) parentMap.put(bone.name, bone.parent);

        for (GeoModelBone bone : modelBones) {
            Matrix4f world = new Matrix4f(localMatrices.get(bone.name));
            String parent = parentMap.get(bone.name);
            while (parent != null && worldMatrices.containsKey(parent)) {
                world = new Matrix4f(worldMatrices.get(parent)).mul(world);
                parent = parentMap.get(parent);
            }
            worldMatrices.put(bone.name, world);
        }
        return worldMatrices;
    }

    private Matrix4f computeLocalMatrix(GeoModelBone bone, AnimClip clip, double t) {
        float rotX = bone.rotX, rotY = bone.rotY, rotZ = bone.rotZ;
        float posX = 0, posY = 0, posZ = 0;
        float scaleX = 1, scaleY = 1, scaleZ = 1;

        if (clip != null) {
            BoneTrack track = clip.bones.get(bone.name);
            if (track != null) {
                float[] rot   = sampleChannel(track.rotation,  t, new float[]{rotX, rotY, rotZ});
                float[] pos   = sampleChannel(track.position,  t, new float[]{0, 0, 0});
                float[] scale = sampleChannel(track.scale,     t, new float[]{1, 1, 1});
                rotX = rot[0]; rotY = rot[1]; rotZ = rot[2];
                posX = pos[0]; posY = pos[1]; posZ = pos[2];
                scaleX = scale[0]; scaleY = scale[1]; scaleZ = scale[2];
            }
        }

        // Build matrix: translate to pivot, rotate, scale, translate back, then apply position offset
        return new Matrix4f()
                .translate(bone.pivotX + posX, bone.pivotY + posY, bone.pivotZ + posZ)
                .rotate(new Quaternionf()
                        .rotateXYZ((float) Math.toRadians(rotX),
                                   (float) Math.toRadians(rotY),
                                   (float) Math.toRadians(rotZ)))
                .scale(scaleX, scaleY, scaleZ)
                .translate(-bone.pivotX, -bone.pivotY, -bone.pivotZ);
    }

    /** Linearly interpolates between keyframes, returning Molang-evaluated XYZ at time {@code t}. */
    private float[] sampleChannel(List<Keyframe> frames, double t, float[] fallback) {
        if (frames.isEmpty()) return fallback;
        if (frames.size() == 1) return eval3(frames.get(0).value, t);

        for (int i = 0; i < frames.size() - 1; i++) {
            Keyframe a = frames.get(i);
            Keyframe b = frames.get(i + 1);
            if (t >= a.time && t <= b.time) {
                double span   = b.time - a.time;
                double alpha  = (span > 0) ? (t - a.time) / span : 0;
                float[] va = eval3(a.value, t);
                float[] vb = eval3(b.value, t);
                return new float[]{
                    lerp(va[0], vb[0], (float) alpha),
                    lerp(va[1], vb[1], (float) alpha),
                    lerp(va[2], vb[2], (float) alpha)
                };
            }
        }
        return eval3(frames.get(frames.size() - 1).value, t);
    }

    private float[] eval3(Vec3Track v, double t) {
        return new float[]{
            (float) v.x.evaluate(t),
            (float) v.y.evaluate(t),
            (float) v.z.evaluate(t)
        };
    }

    private float lerp(float a, float b, float t) {
        return a + (b - a) * t;
    }

    /** Returns {@code true} if an animation is currently playing. */
    public boolean isPlaying() { return playing; }

    /** Returns the currently playing animation name, or {@code null}. */
    public String getCurrentAnimation() { return currentAnim; }

    /** Returns the current animation elapsed time in seconds. */
    public double getAnimTime() { return animTime; }

    /** Returns an unmodifiable view of the loaded animation names. */
    public java.util.Set<String> getLoadedAnimations() {
        return Collections.unmodifiableSet(clips.keySet());
    }
}
