package com.overmind.java;

import com.overmind.api.VertexGraphManager;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.nio.charset.StandardCharsets;
import java.util.UUID;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Handles the Minecraft 1.21.4 Play state.
 *
 * <p>On entry (handlerAdded):
 * <ol>
 *   <li>Send Login (Play) (0x2B) — entity ID, game mode, world info
 *   <li>Send Synchronize Player Position (0x40) — spawn point + velocity (required since 1.21.2)
 *   <li>Schedule periodic Keep Alive (S→C 0x26) every 10 seconds
 *   <li>Load spawn chunks via VertexGraphManager, send each chunk with ChunkHandler logic
 * </ol>
 *
 * <p>Inbound packets handled:
 * <ul>
 *   <li>0x00 Confirm Teleport — echoes the teleport ID we sent in Sync Position
 *   <li>0x18 Keep Alive (C→S) — echo of our Keep Alive payload; logged only
 * </ul>
 */
public class PlayHandler extends ChannelInboundHandlerAdapter {
    private static final Logger logger = LoggerFactory.getLogger(PlayHandler.class);

    private static final int ENTITY_ID        = 1;
    private static final double SPAWN_X       = 0.5;
    private static final double SPAWN_Y       = 100.0;
    private static final double SPAWN_Z       = 0.5;
    private static final float  SPAWN_YAW     = 0.0f;
    private static final float  SPAWN_PITCH   = 0.0f;
    private static final int    TELEPORT_ID   = 1;

    private final String username;
    private final VertexGraphManager vertexGraphManager;
    private final PlayerRegistry playerRegistry;
    private final AtomicLong lastKeepAliveId = new AtomicLong(0);

    private PlayerRegistry.PlayerEntry myEntry;

    public PlayHandler(String username, VertexGraphManager vertexGraphManager,
                       PlayerRegistry playerRegistry) {
        this.username = username;
        this.vertexGraphManager = vertexGraphManager;
        this.playerRegistry = playerRegistry;
    }

    // -------------------------------------------------------------------------
    // Lifecycle
    // -------------------------------------------------------------------------

    @Override
    public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
        logger.info("Play state entered for {}", username);
        sendLoginPlay(ctx);
        sendSyncPosition(ctx);
        scheduleKeepAlive(ctx);
        loadAndSendSpawnChunks(ctx);
        registerWithRegistry(ctx);
    }

    @Override
    public void channelInactive(ChannelHandlerContext ctx) throws Exception {
        if (playerRegistry != null && myEntry != null) {
            playerRegistry.unregister(username);
        }
        logger.info("Java player '{}' disconnected", username);
    }

    // -------------------------------------------------------------------------
    // Inbound
    // -------------------------------------------------------------------------

    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
        if (!(msg instanceof ByteBuf)) return;
        ByteBuf buf = (ByteBuf) msg;
        try {
            int packetLength = readVarInt(buf);
            int packetId     = readVarInt(buf);
            logger.debug("Play packet: length={}, id=0x{}", packetLength, Integer.toHexString(packetId));

            switch (packetId) {
                case PacketConstants.PLAY_CONFIRM_TELEPORT:
                    int teleportId = readVarInt(buf);
                    logger.debug("Confirm Teleport: id={} from {}", teleportId, username);
                    break;

                case PacketConstants.PLAY_KEEP_ALIVE_SERVERBOUND:
                    long keepAliveId = buf.readLong();
                    logger.debug("Keep Alive ACK: id={} from {}", keepAliveId, username);
                    break;

                case PacketConstants.PLAY_SET_PLAYER_POSITION:
                    handleSetPlayerPosition(buf);
                    break;

                case PacketConstants.PLAY_SET_PLAYER_POS_ROT:
                    handleSetPlayerPosRot(buf);
                    break;

                case PacketConstants.PLAY_SET_PLAYER_ROTATION:
                    handleSetPlayerRotation(buf);
                    break;

                case PacketConstants.PLAY_INTERACT:
                    handleInteract(ctx, buf);
                    break;

                case PacketConstants.PLAY_SWING_ARM:
                    // arm-swing animation only — no server-side action needed
                    logger.debug("Swing Arm from {}", username);
                    break;

                default:
                    logger.debug("Unhandled play packet 0x{} from {}",
                            Integer.toHexString(packetId), username);
                    break;
            }
        } finally {
            buf.release();
        }
    }

    // -------------------------------------------------------------------------
    // Outbound packets
    // -------------------------------------------------------------------------

    /**
     * Login (Play) — 0x2B (protocol 774 / 1.21.4).
     *
     * <pre>
     * Int     entity_id
     * Boolean is_hardcore           false
     * VarInt  dimension_count       1
     * Identifier[] dimension_names  ["minecraft:overworld"]
     * VarInt  max_players           100  (ignored by client since 1.16)
     * VarInt  view_distance         8
     * VarInt  simulation_distance   8
     * Boolean reduced_debug_info    false
     * Boolean enable_respawn_screen true
     * Boolean do_limited_crafting   false
     * VarInt  dimension_type        0  (index into registry; 0 = overworld)
     * Identifier dimension_name     "minecraft:overworld"
     * Long    hashed_seed           0
     * Byte    game_mode             0 (Survival)
     * Byte    previous_game_mode    -1 (none)
     * Boolean is_debug              false
     * Boolean is_flat               false
     * Boolean has_death_location    false
     * VarInt  portal_cooldown       0
     * VarInt  sea_level             63
     * Boolean enforces_secure_chat  false
     * </pre>
     */
    private void sendLoginPlay(ChannelHandlerContext ctx) throws Exception {
        ByteBuf payload = ctx.alloc().buffer();
        writeVarInt(payload, PacketConstants.PLAY_LOGIN);

        payload.writeInt(ENTITY_ID);            // entity id
        payload.writeBoolean(false);            // is_hardcore
        writeVarInt(payload, 1);                // dimension_count
        writeString(payload, "minecraft:overworld"); // dimension name
        writeVarInt(payload, 100);              // max_players (ignored)
        writeVarInt(payload, 8);                // view_distance
        writeVarInt(payload, 8);                // simulation_distance
        payload.writeBoolean(false);            // reduced_debug_info
        payload.writeBoolean(true);             // enable_respawn_screen
        payload.writeBoolean(false);            // do_limited_crafting
        writeVarInt(payload, 0);                // dimension_type (0 = overworld)
        writeString(payload, "minecraft:overworld"); // dimension name (current)
        payload.writeLong(0L);                  // hashed_seed
        payload.writeByte(0);                   // game_mode: 0 = Survival
        payload.writeByte(-1);                  // previous game_mode: -1 = none
        payload.writeBoolean(false);            // is_debug
        payload.writeBoolean(false);            // is_flat
        payload.writeBoolean(false);            // has_death_location
        writeVarInt(payload, 0);                // portal_cooldown
        writeVarInt(payload, 63);               // sea_level
        payload.writeBoolean(false);            // enforces_secure_chat

        ctx.writeAndFlush(frame(ctx, payload));
        logger.info("Login (Play) sent to {}", username);
    }

    /**
     * Synchronize Player Position — 0x40 (protocol 774 / 1.21.4).
     *
     * <p>Since 1.21.2 the packet includes velocity fields and the Teleport ID
     * moved to the END of the packet.
     *
     * <pre>
     * Double  x
     * Double  y
     * Double  z
     * Double  velocity_x
     * Double  velocity_y
     * Double  velocity_z
     * Float   yaw
     * Float   pitch
     * Int     flags          0 = all values are absolute
     * VarInt  teleport_id    ← at the end since 1.21.2
     * </pre>
     */
    private void sendSyncPosition(ChannelHandlerContext ctx) throws Exception {
        ByteBuf payload = ctx.alloc().buffer();
        writeVarInt(payload, PacketConstants.PLAY_SYNC_POSITION);

        payload.writeDouble(SPAWN_X);           // x
        payload.writeDouble(SPAWN_Y);           // y
        payload.writeDouble(SPAWN_Z);           // z
        payload.writeDouble(0.0);               // velocity x
        payload.writeDouble(0.0);               // velocity y
        payload.writeDouble(0.0);               // velocity z
        payload.writeFloat(SPAWN_YAW);          // yaw
        payload.writeFloat(SPAWN_PITCH);        // pitch
        payload.writeInt(0);                    // flags (0 = all absolute)
        writeVarInt(payload, TELEPORT_ID);      // teleport id (client echoes in Confirm Teleport)

        ctx.writeAndFlush(frame(ctx, payload));
        logger.info("Synchronize Player Position sent to {} at ({}, {}, {})", username, SPAWN_X, SPAWN_Y, SPAWN_Z);
    }

    /**
     * Keep Alive (S→C) — 0x26.
     * <pre>
     * Long  keep_alive_id
     * </pre>
     */
    private void sendKeepAlive(ChannelHandlerContext ctx) throws Exception {
        long id = System.currentTimeMillis();
        lastKeepAliveId.set(id);
        ByteBuf payload = ctx.alloc().buffer();
        writeVarInt(payload, PacketConstants.PLAY_KEEP_ALIVE_CLIENTBOUND);
        payload.writeLong(id);
        ctx.writeAndFlush(frame(ctx, payload));
        logger.debug("Keep Alive sent to {}: id={}", username, id);
    }

    private void scheduleKeepAlive(ChannelHandlerContext ctx) {
        ctx.executor().scheduleAtFixedRate(() -> {
            if (ctx.channel().isActive()) {
                try {
                    sendKeepAlive(ctx);
                } catch (Exception e) {
                    logger.error("Failed to send Keep Alive to {}", username, e);
                    ctx.close();
                }
            }
        }, 10, 10, TimeUnit.SECONDS);
    }

    /**
     * Loads the spawn area chunks and sends each one to the client via
     * {@link ChunkHandler#sendChunkToJavaClient}.
     */
    private void loadAndSendSpawnChunks(ChannelHandlerContext ctx) throws Exception {
        int viewDistance = vertexGraphManager.getViewDistance();

        // Tell the client that chunk (0, 0) is the view centre before sending any data.
        sendSetCenterChunk(ctx, 0, 0);

        vertexGraphManager.loadArea(0, 0, viewDistance);

        ChunkHandler chunkHandler = new ChunkHandler(vertexGraphManager);
        for (int cx = -viewDistance; cx <= viewDistance; cx++) {
            for (int cz = -viewDistance; cz <= viewDistance; cz++) {
                chunkHandler.sendChunkToJavaClient(ctx, cx, cz);
            }
        }
        logger.info("Spawn chunks sent to {} ({}x{})", username, viewDistance * 2 + 1, viewDistance * 2 + 1);
    }

    /**
     * Set Center Chunk (0x54) — required before chunk data so the client
     * knows which chunks belong to its view and which to unload.
     *
     * <pre>
     * VarInt  chunkX
     * VarInt  chunkZ
     * </pre>
     */
    private void sendSetCenterChunk(ChannelHandlerContext ctx, int chunkX, int chunkZ) throws Exception {
        ByteBuf payload = ctx.alloc().buffer();
        writeVarInt(payload, PacketConstants.PLAY_SET_CENTER_CHUNK);
        writeVarInt(payload, chunkX);
        writeVarInt(payload, chunkZ);
        ctx.writeAndFlush(frame(ctx, payload));
        logger.debug("Set Center Chunk ({}, {}) sent to {}", chunkX, chunkZ, username);
    }

    // -------------------------------------------------------------------------
    // Combat / Phase E
    // -------------------------------------------------------------------------

    /**
     * Handles an Interact packet (C→S 0x16).
     *
     * <p>Action types:
     * <ul>
     *   <li>0 = INTERACT (right-click use)</li>
     *   <li>1 = ATTACK (left-click hit)</li>
     *   <li>2 = INTERACT_AT (right-click at specific position)</li>
     * </ul>
     *
     */
    private void handleInteract(ChannelHandlerContext ctx, ByteBuf buf) throws Exception {
        int targetEntityId = readVarInt(buf);
        int actionType     = readVarInt(buf);
        // Boolean "sneaking" follows — not used server-side here

        if (actionType == 1) { // ATTACK
            logger.debug("Attack from {} on entity {}", username, targetEntityId);
            // Future: route to entity damage pipeline
        } else {
            logger.debug("Interact type={} on entity {} from {}", actionType, targetEntityId, username);
        }
    }

    // -------------------------------------------------------------------------
    // Position handling (C→S)
    // -------------------------------------------------------------------------

    private void handleSetPlayerPosition(ByteBuf buf) {
        double x = buf.readDouble();
        double y = buf.readDouble();
        double z = buf.readDouble();
        // boolean onGround follows — not tracked server-side here
        if (playerRegistry != null && myEntry != null) {
            playerRegistry.updatePosition(myEntry, x, y, z, myEntry.yaw, myEntry.pitch, myEntry.headYaw);
        }
    }

    private void handleSetPlayerPosRot(ByteBuf buf) {
        double x     = buf.readDouble();
        double y     = buf.readDouble();
        double z     = buf.readDouble();
        float  yaw   = buf.readFloat();
        float  pitch = buf.readFloat();
        if (playerRegistry != null && myEntry != null) {
            playerRegistry.updatePosition(myEntry, x, y, z, yaw, pitch, yaw);
        }
    }

    private void handleSetPlayerRotation(ByteBuf buf) {
        float yaw   = buf.readFloat();
        float pitch = buf.readFloat();
        if (playerRegistry != null && myEntry != null) {
            playerRegistry.updatePosition(myEntry,
                    myEntry.x, myEntry.y, myEntry.z, yaw, pitch, yaw);
        }
    }

    // -------------------------------------------------------------------------
    // PlayerRegistry integration
    // -------------------------------------------------------------------------

    private void registerWithRegistry(ChannelHandlerContext ctx) {
        if (playerRegistry == null) return;
        myEntry = playerRegistry.register(username, false, SPAWN_X, SPAWN_Y, SPAWN_Z,
                new PlayerRegistry.PlayerListener() {
                    @Override
                    public void onPlayerJoined(PlayerRegistry.PlayerEntry joiner) {
                        ctx.channel().eventLoop().execute(() -> {
                            try { sendSpawnPlayer(ctx, joiner); } catch (Exception e) {
                                logger.error("Failed to spawn {} for {}", joiner.username, username, e);
                            }
                        });
                    }
                    @Override
                    public void onPlayerLeft(PlayerRegistry.PlayerEntry leaver) {
                        ctx.channel().eventLoop().execute(() -> {
                            try { sendDespawnPlayer(ctx, leaver); } catch (Exception e) {
                                logger.error("Failed to despawn {} for {}", leaver.username, username, e);
                            }
                        });
                    }
                    @Override
                    public void onPlayerMoved(PlayerRegistry.PlayerEntry mover) {
                        ctx.channel().eventLoop().execute(() -> {
                            try { sendMovePlayer(ctx, mover); } catch (Exception e) {
                                logger.warn("Failed to move {} for {}", mover.username, username, e);
                            }
                        });
                    }
                });
    }

    // -------------------------------------------------------------------------
    // S→C player visibility packets
    // -------------------------------------------------------------------------

    /**
     * Sends PlayerInfoUpdate (ADD_PLAYER | UPDATE_LISTED) followed by SpawnEntity
     * so that {@code other} appears in this client's world.
     */
    private void sendSpawnPlayer(ChannelHandlerContext ctx,
                                 PlayerRegistry.PlayerEntry other) throws Exception {
        UUID uuid = playerUuid(other.username);

        // 1. Player Info Update (0x3E) — must precede Spawn Entity for player entities
        ByteBuf info = ctx.alloc().buffer();
        writeVarInt(info, PacketConstants.PLAY_PLAYER_INFO_UPDATE);
        info.writeByte(0x09);                       // actions: ADD_PLAYER (0x01) | UPDATE_LISTED (0x08)
        writeVarInt(info, 1);                       // 1 player
        info.writeLong(uuid.getMostSignificantBits());
        info.writeLong(uuid.getLeastSignificantBits());
        writeString(info, other.username);          // ADD_PLAYER: name
        writeVarInt(info, 0);                       // ADD_PLAYER: 0 properties (offline mode)
        info.writeBoolean(true);                    // UPDATE_LISTED: listed = true
        ctx.write(frame(ctx, info));

        // 2. Spawn Entity (0x01)
        ByteBuf spawn = ctx.alloc().buffer();
        writeVarInt(spawn, PacketConstants.PLAY_SPAWN_ENTITY);
        writeVarInt(spawn, other.entityId);
        spawn.writeLong(uuid.getMostSignificantBits());
        spawn.writeLong(uuid.getLeastSignificantBits());
        writeVarInt(spawn, PacketConstants.ENTITY_TYPE_PLAYER);
        spawn.writeDouble(other.x);
        spawn.writeDouble(other.y);
        spawn.writeDouble(other.z);
        spawn.writeByte(angleToByte(other.pitch));
        spawn.writeByte(angleToByte(other.yaw));
        spawn.writeByte(angleToByte(other.headYaw));
        writeVarInt(spawn, 0);                      // data
        spawn.writeShort(0);                        // velocity x
        spawn.writeShort(0);                        // velocity y
        spawn.writeShort(0);                        // velocity z
        ctx.writeAndFlush(frame(ctx, spawn));

        logger.debug("Spawned {} for {}", other.username, username);
    }

    /**
     * Sends RemoveEntities (0x42) and PlayerInfoRemove (0x3C) to despawn {@code other}.
     */
    private void sendDespawnPlayer(ChannelHandlerContext ctx,
                                   PlayerRegistry.PlayerEntry other) throws Exception {
        // Remove from world
        ByteBuf remove = ctx.alloc().buffer();
        writeVarInt(remove, PacketConstants.PLAY_REMOVE_ENTITIES);
        writeVarInt(remove, 1);                     // 1 entity
        writeVarInt(remove, other.entityId);
        ctx.write(frame(ctx, remove));

        // Remove from tab list
        UUID uuid = playerUuid(other.username);
        ByteBuf infoRemove = ctx.alloc().buffer();
        writeVarInt(infoRemove, PacketConstants.PLAY_PLAYER_INFO_REMOVE);
        writeVarInt(infoRemove, 1);                 // 1 UUID
        infoRemove.writeLong(uuid.getMostSignificantBits());
        infoRemove.writeLong(uuid.getLeastSignificantBits());
        ctx.writeAndFlush(frame(ctx, infoRemove));

        logger.debug("Despawned {} for {}", other.username, username);
    }

    /**
     * Sends TeleportEntity (0x6E) + RotateHead (0x46) to update {@code mover}'s position.
     */
    private void sendMovePlayer(ChannelHandlerContext ctx,
                                PlayerRegistry.PlayerEntry mover) throws Exception {
        // Teleport Entity (absolute coords — no delta encoding needed)
        ByteBuf teleport = ctx.alloc().buffer();
        writeVarInt(teleport, PacketConstants.PLAY_TELEPORT_ENTITY);
        writeVarInt(teleport, mover.entityId);
        teleport.writeDouble(mover.x);
        teleport.writeDouble(mover.y);
        teleport.writeDouble(mover.z);
        writeVarInt(teleport, 0);                   // velocity x (fixed-point, 0 = stationary)
        writeVarInt(teleport, 0);                   // velocity y
        writeVarInt(teleport, 0);                   // velocity z
        teleport.writeByte(angleToByte(mover.yaw));
        teleport.writeByte(angleToByte(mover.pitch));
        teleport.writeBoolean(true);                // on ground
        ctx.write(frame(ctx, teleport));

        // Rotate Head — syncs the head yaw independently of body yaw
        ByteBuf head = ctx.alloc().buffer();
        writeVarInt(head, PacketConstants.PLAY_ROTATE_HEAD);
        writeVarInt(head, mover.entityId);
        head.writeByte(angleToByte(mover.headYaw));
        ctx.writeAndFlush(frame(ctx, head));
    }

    // ─── Angle / UUID helpers ──────────────────────────────────────────────────

    /** Converts a degrees float to a Minecraft Angle byte (256 steps per 360°). */
    private static byte angleToByte(float degrees) {
        return (byte) Math.round(degrees * 256.0f / 360.0f);
    }

    /** Returns a deterministic offline-mode UUID derived from the player name. */
    private static UUID playerUuid(String username) {
        return UUID.nameUUIDFromBytes(("OfflinePlayer:" + username)
                .getBytes(StandardCharsets.UTF_8));
    }

    // -------------------------------------------------------------------------
    // Helpers
    // -------------------------------------------------------------------------

    private ByteBuf frame(ChannelHandlerContext ctx, ByteBuf payload) {
        ByteBuf packet = ctx.alloc().buffer();
        writeVarInt(packet, payload.readableBytes());
        packet.writeBytes(payload);
        payload.release();
        return packet;
    }

    private void writeString(ByteBuf out, String str) throws Exception {
        byte[] bytes = str.getBytes("UTF-8");
        writeVarInt(out, bytes.length);
        out.writeBytes(bytes);
    }

    private void writeVarInt(ByteBuf out, int value) {
        while (true) {
            if ((value & 0xFFFFFF80) == 0) {
                out.writeByte(value);
                return;
            }
            out.writeByte((value & 0x7F) | 0x80);
            value >>>= 7;
        }
    }

    private int readVarInt(ByteBuf in) {
        int value = 0;
        int position = 0;
        byte currentByte;
        do {
            if (position >= PacketConstants.VARINT_MAX_POSITION) {
                throw new RuntimeException("VarInt too big");
            }
            currentByte = in.readByte();
            value |= (currentByte & 0x7F) << position;
            if ((currentByte & 0x80) == 0) break;
            position += 7;
        } while (true);
        return value;
    }

    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
        logger.error("Exception in Play handler for {}", username, cause);
        ctx.close();
    }
}
