package com.overmind.java;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import org.joml.Matrix4f;
import org.joml.Quaternionf;
import org.joml.Vector3f;

import java.util.Map;

/**
 * Builds Minecraft 1.21.4 packets for {@code item_display} entities.
 *
 * <p>Item Display entities (added in 1.19.4) render an item stack with a fully configurable
 * affine transformation, making them ideal for showing Bedrock entity models to Java clients.
 *
 * <h3>Packets used</h3>
 * <ul>
 *   <li><b>0x01 Spawn Entity</b> — creates the display entity in the world</li>
 *   <li><b>0x56 Set Entity Metadata</b> — sets the transformation and item</li>
 *   <li><b>0x04 Remove Entities</b> — despawns the entity when the Bedrock entity leaves</li>
 * </ul>
 *
 * <h3>Entity type ID</h3>
 * {@code item_display} entity type ID in 1.21.4 = 1 (first entity registered).
 * Verify against {@code generated/reports/registries.json} from the vanilla 1.21.4 server.
 *
 * <h3>Transformation metadata index</h3>
 * Display entity metadata slots (base Entity = 0–7, Display = 8–14, Item Display = 22–23):
 * <ul>
 *   <li>Index 8: Transformation (translation, left-rotation, scale, right-rotation)</li>
 *   <li>Index 9: Billboard Constraints (byte, 0 = fixed)</li>
 *   <li>Index 11: View Range (float)</li>
 *   <li>Index 22: Item (Slot)</li>
 *   <li>Index 23: Item Display Transform (byte, 0 = NONE)</li>
 * </ul>
 */
public final class ItemDisplayPacketBuilder {

    /**
     * Entity type ID for {@code item_display} in protocol 774 (1.21.4).
     * Update if the registry order shifts in a future version.
     */
    public static final int ENTITY_TYPE_ITEM_DISPLAY = 1;

    // Packet IDs (Play state, S→C)
    private static final int PKT_SPAWN_ENTITY   = 0x01;
    private static final int PKT_SET_METADATA   = 0x56;
    private static final int PKT_REMOVE_ENTITIES = 0x04;

    // Entity metadata type tags (used in Set Entity Metadata entries)
    private static final int META_TYPE_TRANSFORMATION = 24; // Transformation (T+LR+S+RR)

    private ItemDisplayPacketBuilder() {}

    // ── Spawn ────────────────────────────────────────────────────────────────

    /**
     * Builds a framed {@code Spawn Entity} (0x01) packet for one Item Display bone.
     *
     * @param alloc    Netty allocator
     * @param entityId server-assigned entity ID for this display bone
     * @param x        world X position (in blocks)
     * @param y        world Y position (in blocks)
     * @param z        world Z position (in blocks)
     */
    public static ByteBuf buildSpawnPacket(ByteBufAllocator alloc,
                                           int entityId,
                                           double x, double y, double z) {
        ByteBuf payload = alloc.buffer();
        writeVarInt(payload, PKT_SPAWN_ENTITY);
        writeVarInt(payload, entityId);           // Entity ID
        writeUuid(payload, 0L, (long) entityId);  // UUID (use entity-ID-based fake UUID)
        writeVarInt(payload, ENTITY_TYPE_ITEM_DISPLAY); // Entity type
        payload.writeDouble(x);                   // X
        payload.writeDouble(y);                   // Y
        payload.writeDouble(z);                   // Z
        payload.writeByte(0);                     // Pitch (0)
        payload.writeByte(0);                     // Yaw (0)
        payload.writeByte(0);                     // Head yaw (0)
        writeVarInt(payload, 0);                  // Data (0 = no extra data)
        payload.writeShort(0);                    // Velocity X
        payload.writeShort(0);                    // Velocity Y
        payload.writeShort(0);                    // Velocity Z
        return framePacket(alloc, payload);
    }

    // ── Transformation ───────────────────────────────────────────────────────

    /**
     * Builds a framed {@code Set Entity Metadata} (0x56) packet that applies a
     * JOML {@link Matrix4f} as the Item Display entity's transformation.
     *
     * <p>The matrix is decomposed into translation, left-rotation (quaternion), scale, and
     * right-rotation (identity quaternion) as required by the Display entity metadata format.
     *
     * @param alloc        Netty allocator
     * @param entityId     entity whose transformation to update
     * @param transform    world-space transform matrix (from {@link com.overmind.api.AnimationController})
     */
    public static ByteBuf buildTransformationPacket(ByteBufAllocator alloc,
                                                     int entityId,
                                                     Matrix4f transform) {
        Vector3f translation = new Vector3f();
        Quaternionf leftRot  = new Quaternionf();
        Vector3f scale       = new Vector3f();
        transform.getTranslation(translation);
        transform.getUnnormalizedRotation(leftRot);
        transform.getScale(scale);

        ByteBuf payload = alloc.buffer();
        writeVarInt(payload, PKT_SET_METADATA);
        writeVarInt(payload, entityId);

        // Metadata entry: index 8 = Transformation
        payload.writeByte(8);                     // index
        writeVarInt(payload, META_TYPE_TRANSFORMATION); // type tag
        payload.writeFloat(translation.x);
        payload.writeFloat(translation.y);
        payload.writeFloat(translation.z);
        payload.writeFloat(leftRot.x);            // left rotation quaternion
        payload.writeFloat(leftRot.y);
        payload.writeFloat(leftRot.z);
        payload.writeFloat(leftRot.w);
        payload.writeFloat(scale.x);
        payload.writeFloat(scale.y);
        payload.writeFloat(scale.z);
        payload.writeFloat(0f);                   // right rotation (identity)
        payload.writeFloat(0f);
        payload.writeFloat(0f);
        payload.writeFloat(1f);

        payload.writeByte(0xFF);                  // end of metadata list
        return framePacket(alloc, payload);
    }

    /**
     * Sends transformation updates for all bones in the given map to a client.
     * The map comes from {@link com.overmind.api.AnimationController#getBoneMatrices()}.
     *
     * @param alloc          Netty allocator
     * @param boneEntityIds  mapping of bone name → entity ID assigned when the mob spawned
     * @param boneMatrices   per-bone world-space matrices from AnimationController
     * @param baseX/Y/Z      world position of the root entity (Bedrock mob's position)
     */
    public static ByteBuf[] buildBoneUpdatePackets(ByteBufAllocator alloc,
                                                    Map<String, Integer> boneEntityIds,
                                                    Map<String, org.joml.Matrix4f> boneMatrices,
                                                    double baseX, double baseY, double baseZ) {
        ByteBuf[] packets = new ByteBuf[boneEntityIds.size()];
        int i = 0;
        for (Map.Entry<String, Integer> entry : boneEntityIds.entrySet()) {
            String boneName = entry.getKey();
            int entityId    = entry.getValue();
            Matrix4f mat    = boneMatrices.getOrDefault(boneName, new Matrix4f());
            // Offset matrix by root entity position (convert Bedrock model units to blocks: ÷16)
            Matrix4f worldMat = new Matrix4f(mat).translate(
                    (float) baseX, (float) baseY, (float) baseZ);
            packets[i++] = buildTransformationPacket(alloc, entityId, worldMat);
        }
        return packets;
    }

    // ── Despawn ──────────────────────────────────────────────────────────────

    /**
     * Builds a framed {@code Remove Entities} (0x04) packet to despawn all display bones
     * for one Bedrock entity.
     *
     * @param alloc        Netty allocator
     * @param entityIds    list of entity IDs to remove
     */
    public static ByteBuf buildRemovePacket(ByteBufAllocator alloc, int... entityIds) {
        ByteBuf payload = alloc.buffer();
        writeVarInt(payload, PKT_REMOVE_ENTITIES);
        writeVarInt(payload, entityIds.length);
        for (int id : entityIds) writeVarInt(payload, id);
        return framePacket(alloc, payload);
    }

    // ── Helpers ──────────────────────────────────────────────────────────────

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

    private static void writeUuid(ByteBuf buf, long msb, long lsb) {
        buf.writeLong(msb);
        buf.writeLong(lsb);
    }
}
