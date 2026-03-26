package com.overmind.java;

/**
 * Shared packet-level constants used across all Netty handlers.
 * Packet IDs target Java Edition 1.21.11 (protocol 774).
 */
public final class PacketConstants {

    /** Maximum number of bytes a VarInt may occupy (Minecraft protocol). */
    public static final int VARINT_MAX_BYTES = 5;

    /**
     * Maximum bit-position a VarInt shift may reach before the value is considered
     * malformed. Each byte contributes 7 value bits, so the limit is
     * {@code VARINT_MAX_BYTES * 7 = 35}.
     * Use in readVarInt loops: {@code if (position >= VARINT_MAX_POSITION) throw ...}
     */
    public static final int VARINT_MAX_POSITION = VARINT_MAX_BYTES * 7; // 35

    // ── Configuration state (C→S) ────────────────────────────────────────────
    /** Serverbound Known Packs — client sends this in response to Clientbound Known Packs. */
    public static final int CONFIG_SERVERBOUND_KNOWN_PACKS = 0x07;
    /** Serverbound Acknowledge Finish Configuration — client signals it has entered Play. */
    public static final int CONFIG_SERVERBOUND_ACK_FINISH   = 0x03;

    // ── Configuration state (S→C) ────────────────────────────────────────────
    /** Clientbound Known Packs — server sends the list of data-packs it supports. */
    public static final int CONFIG_CLIENTBOUND_KNOWN_PACKS  = 0x0E;
    /** Clientbound Finish Configuration — server signals config is done. */
    public static final int CONFIG_CLIENTBOUND_FINISH       = 0x03;

    // ── Play state (S→C) ─────────────────────────────────────────────────────
    /** Login (Play) — first Play-state packet from server; sets entity ID, world info, etc. */
    public static final int PLAY_LOGIN                      = 0x30;
    /** Synchronize Player Position — teleports the player to the spawn point. */
    public static final int PLAY_SYNC_POSITION              = 0x46;
    /** Keep Alive (server→client) — server heartbeat; client must echo it back. */
    public static final int PLAY_KEEP_ALIVE_CLIENTBOUND     = 0x2B;

    /**
     * Set Center Chunk (server→client) — tells the client which chunk is the view centre.
     * Must be sent before chunk data so the client knows which chunks to keep loaded.
     * Packet ID 0x5C in 1.21.11 (protocol 774).
     */
    public static final int PLAY_SET_CENTER_CHUNK           = 0x5C;

    // ── Play state (C→S) ─────────────────────────────────────────────────────
    /** Confirm Teleport — client acks a Synchronize Player Position packet. */
    public static final int PLAY_CONFIRM_TELEPORT           = 0x00;
    /**
     * Keep Alive (client→server) — client echo of the server's Keep Alive payload.
     * In 1.21.11 (protocol 774) this is 0x1B; 0x1E is Set Player Position And Rotation.
     */
    public static final int PLAY_KEEP_ALIVE_SERVERBOUND     = 0x1B;

    // ── Play state (C→S) — player movement ───────────────────────────────────
    /** Set Player Position (C→S 0x1D) — position-only update (no rotation change). */
    public static final int PLAY_SET_PLAYER_POSITION        = 0x1D;
    /** Set Player Position And Rotation (C→S 0x1E) — full position + yaw/pitch update. */
    public static final int PLAY_SET_PLAYER_POS_ROT         = 0x1E;
    /** Set Player Rotation (C→S 0x1F) — rotation-only update (no position change). */
    public static final int PLAY_SET_PLAYER_ROTATION        = 0x1F;
    /**
     * Interact — sent when the client attacks or interacts with an entity.
     * Body: VarInt entityId, VarInt actionType (0=INTERACT, 1=ATTACK, 2=INTERACT_AT), Boolean sneaking.
     */
    public static final int PLAY_INTERACT                   = 0x19;
    /** Swing Arm (C→S) — client swings the held item; precedes or accompanies an attack. */
    public static final int PLAY_SWING_ARM                  = 0x3C;

    // ── Play state (S→C) — player visibility ─────────────────────────────────
    /**
     * Spawn Entity (S→C 0x01) — spawns any entity (including players) in the world.
     * Body: VarInt entityId, UUID, VarInt type, Double x/y/z, Angle pitch/yaw/headYaw,
     *       VarInt data, Short velX/Y/Z.
     */
    public static final int PLAY_SPAWN_ENTITY               = 0x01;
    /**
     * Player Info Update (S→C 0x44) — adds/updates players in the tab list.
     * Required before Spawn Entity for player entities so the client can load their skin.
     * Actions byte is a bitmask: 0x01=ADD_PLAYER, 0x08=UPDATE_LISTED.
     */
    public static final int PLAY_PLAYER_INFO_UPDATE         = 0x44;
    /**
     * Player Info Remove (S→C 0x43) — removes players from the tab list.
     * Body: VarInt count, UUID[] uuids.
     */
    public static final int PLAY_PLAYER_INFO_REMOVE         = 0x43;
    /**
     * Remove Entities (S→C 0x4B) — despawns entities from the world.
     * Body: VarInt[] entityIds.
     */
    public static final int PLAY_REMOVE_ENTITIES            = 0x4B;
    /**
     * Teleport Entity (S→C 0x7B) — moves an entity to absolute coordinates.
     * Body: VarInt entityId, Double x/y/z, VarInt velX/Y/Z, Angle yaw/pitch, Boolean onGround.
     */
    public static final int PLAY_TELEPORT_ENTITY            = 0x7B;
    /**
     * Rotate Head (S→C 0x51) — updates the head yaw of an entity independently of body yaw.
     * Body: VarInt entityId, Angle headYaw.
     */
    public static final int PLAY_ROTATE_HEAD                = 0x51;

    /**
     * Entity type ID for {@code minecraft:player} in Java 1.21.11 (protocol 774).
     * Verify against the vanilla entity_type registry report if this shifts.
     */
    public static final int ENTITY_TYPE_PLAYER              = 155;

    // ── Play state (S→C) — survival HUD ──────────────────────────────────────
    /**
     * Set Health (S→C 0x2A) — sends the player's current health, food, and saturation.
     * Body: Float health, VarInt food, Float food_saturation.
     * Must be sent on login so the client renders the health/hunger bar correctly.
     */
    public static final int PLAY_SET_HEALTH                 = 0x2A;

    // ── Play state (S→C) — world events ──────────────────────────────────────
    /**
     * Game Event (S→C 0x22) — general-purpose game event packet.
     * Body: VarInt type, Float value.
     * Type 13 (START_WAITING_FOR_LEVEL_CHUNKS) MUST be sent after Login (Play) and
     * before any chunk data so the client exits the loading screen (required since 1.20.3).
     */
    public static final int PLAY_GAME_EVENT                 = 0x22;
    /** Game Event type 13: tells the client to start waiting for level (chunk) data. */
    public static final int GAME_EVENT_START_WAITING_FOR_CHUNKS = 13;

    // ── Play state (S→C) — chunk batching (required since 1.20.3) ─────────────
    /**
     * Chunk Batch Start (S→C 0x0D) — marks the beginning of a batch of Chunk Data packets.
     * Body: (empty). Must be sent before the first chunk in each batch.
     */
    public static final int PLAY_CHUNK_BATCH_START          = 0x0D;
    /**
     * Chunk Batch Finished (S→C 0x0C) — marks the end of a chunk batch.
     * Body: VarInt batchSize (number of chunks in this batch).
     * The client responds with Chunk Batch Received (C→S) to throttle future batches.
     */
    public static final int PLAY_CHUNK_BATCH_FINISHED       = 0x0C;

    // ── Play state (C→S) — chunk batch acknowledgement ───────────────────────
    /**
     * Chunk Batch Received (C→S) — client sends this after receiving Chunk Batch Finished.
     * Body: Float desiredChunksPerTick. Used for flow control; safe to ignore server-side.
     * Exact packet ID may vary by protocol version; log-and-discard is acceptable.
     */
    public static final int PLAY_CHUNK_BATCH_RECEIVED       = 0x08;

    // ── Play state (S→C) — combat feedback ───────────────────────────────────
    /**
     * Entity Animation (S→C 0x02) — sent to reset the client's attack cooldown indicator.
     * Body: VarInt entityId, VarInt animationId (1 = WAKE_UP / cooldown reset).
     */
    public static final int PLAY_ENTITY_ANIMATION           = 0x02;

    // ── Bedrock RakNet packet IDs ─────────────────────────────────────────────
    /** RakNet: Unconnected Ping (sent by client; timestamp + magic). */
    public static final int RAKNET_UNCONNECTED_PING         = 0x01;
    /** RakNet: Unconnected Pong (server reply with server GUID + server name). */
    public static final int RAKNET_UNCONNECTED_PONG         = 0x1C;
    /** RakNet: Open Connection Request 1 (MTU probe from client). */
    public static final int RAKNET_OPEN_CONNECTION_REQ_1    = 0x05;
    /** RakNet: Open Connection Reply 1 (server accepts MTU). */
    public static final int RAKNET_OPEN_CONNECTION_REPLY_1  = 0x06;
    /** RakNet: Open Connection Request 2 (client finalises connection with chosen MTU). */
    public static final int RAKNET_OPEN_CONNECTION_REQ_2    = 0x07;
    /** RakNet: Open Connection Reply 2 (server sends own address + MTU). */
    public static final int RAKNET_OPEN_CONNECTION_REPLY_2  = 0x08;
    /** RakNet: Connection Request (first reliable packet from client with timestamp + GUID). */
    public static final int RAKNET_CONNECTION_REQUEST       = 0x09;
    /** RakNet: Connection Request Accepted (server sends accepted timestamp + server addresses). */
    public static final int RAKNET_CONNECTION_ACCEPTED      = 0x10;
    /** RakNet: New Incoming Connection (client notifies server it has connected). */
    public static final int RAKNET_NEW_INCOMING_CONNECTION  = 0x13;
    /** RakNet: Disconnect Notification. */
    public static final int RAKNET_DISCONNECT               = 0x15;
    /** RakNet: ACK — receiver confirms which datagrams arrived. */
    public static final int RAKNET_ACK                      = 0xC0;
    /** RakNet: NAK — receiver requests retransmission of missing datagrams. */
    public static final int RAKNET_NAK                      = 0xA0;
    /** RakNet: Data packet range (0x80–0x8F) — framed datagrams containing encapsulated packets. */
    public static final int RAKNET_DATA_PACKET_BASE         = 0x80;

    // ── Bedrock game packet IDs (inside RakNet, after 0xFE batch wrapper) ────
    /** Bedrock Login packet — client sends JWT chain and skin data. */
    public static final int BEDROCK_PKT_LOGIN               = 0x01;
    /** Bedrock PlayStatus — server tells client login accepted / spawn ready. */
    public static final int BEDROCK_PKT_PLAY_STATUS         = 0x02;
    /** Bedrock ResourcePacksInfo — server advertises resource packs. */
    public static final int BEDROCK_PKT_RESOURCE_PACKS_INFO = 0x06;
    /** Bedrock ResourcePackStack — ordered pack stack after client confirms pack list. */
    public static final int BEDROCK_PKT_RESOURCE_PACK_STACK = 0x07;
    /** Bedrock ResourcePackClientResponse — client acks or rejects packs. */
    public static final int BEDROCK_PKT_RESOURCE_PACK_RESP  = 0x08;
    /** Bedrock StartGame — full world configuration packet sent after pack negotiation. */
    public static final int BEDROCK_PKT_START_GAME          = 0x0B;
    /** Bedrock RequestChunkRadius — client requests its preferred view distance. */
    public static final int BEDROCK_PKT_REQUEST_CHUNK_RADIUS = 0x45;
    /** Bedrock ChunkRadiusUpdated — server confirms the granted chunk radius. */
    public static final int BEDROCK_PKT_CHUNK_RADIUS_UPDATED = 0x46;
    /** Bedrock LevelChunk — chunk data from server to client. */
    public static final int BEDROCK_PKT_LEVEL_CHUNK         = 0x3A;
    /** Bedrock AddPlayer (S→C 0x0C) — spawns another player entity on the client. */
    public static final int BEDROCK_PKT_ADD_PLAYER          = 0x0C;
    /**
     * Bedrock MovePlayer (0x13) — used both C→S (client position update) and
     * S→C (server moves another player entity on this client).
     */
    public static final int BEDROCK_PKT_MOVE_PLAYER         = 0x13;
    /** Bedrock RemoveEntity (S→C 0x0E) — despawns an entity from the client. */
    public static final int BEDROCK_PKT_REMOVE_ENTITY       = 0x0E;
    /** Bedrock PlayerAction — client sends movement/crouch/sprint events. */
    public static final int BEDROCK_PKT_PLAYER_ACTION       = 0x24;
    /** Bedrock NetworkSettings — compression threshold + algorithm. */
    public static final int BEDROCK_PKT_NETWORK_SETTINGS    = 0x8F;
    /** Bedrock RequestNetworkSettings — client requests settings before Login. */
    public static final int BEDROCK_PKT_REQUEST_NET_SETTINGS = 0xC1;

    // ── Bedrock PlayerAction action IDs ──────────────────────────────────────
    /** PlayerAction: START_SNEAK — player begins crouching (used for shield raise). */
    public static final int BEDROCK_ACTION_START_SNEAK      = 4;
    /** PlayerAction: STOP_SNEAK — player stops crouching (used for shield lower). */
    public static final int BEDROCK_ACTION_STOP_SNEAK       = 5;

    private PacketConstants() {}
}
