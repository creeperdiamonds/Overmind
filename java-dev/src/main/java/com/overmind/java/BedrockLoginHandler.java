package com.overmind.java;

import com.overmind.api.ChunkNode;
import com.overmind.api.VertexGraphManager;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.nio.charset.StandardCharsets;
import java.util.UUID;
import java.util.zip.Deflater;

/**
 * Full RakNet + Bedrock Edition login pipeline for Bedrock 1.21+ clients.
 *
 * <h3>Protocol layers</h3>
 * <ol>
 *   <li><b>RakNet handshake</b> (connectionless unencapsulated packets):
 *       UnconnectedPing → UnconnectedPong → OpenConnectionReq1/2 → OpenConnectionReply1/2</li>
 *   <li><b>RakNet reliability</b> (DATA datagrams 0x80–0x8F):
 *       ConnectionRequest → ConnectionRequestAccepted → NewIncomingConnection</li>
 *   <li><b>Bedrock game login</b> (inside 0xFE batch wrapper, zlib-compressed after NetworkSettings):
 *       RequestNetworkSettings → NetworkSettings → Login → PlayStatus → ResourcePacks →
 *       StartGame → chunks</li>
 * </ol>
 *
 * <h3>Notes on TCP vs UDP</h3>
 * Standard Bedrock clients connect over UDP (port 19132). This handler runs on the TCP pipeline
 * for proxy / tunneled connections detected by {@link ProtocolDetector}. For native Bedrock clients
 * an additional UDP channel is bound in {@link OvermindServer} on port 19132.
 *
 * <h3>Compression</h3>
 * After {@link PacketConstants#BEDROCK_PKT_NETWORK_SETTINGS} is sent with threshold=1, all
 * subsequent game packets are sent inside a 0xFE wrapper with zlib-compressed body. Inbound
 * packets from the client must be decompressed before parsing.
 */
public class BedrockLoginHandler extends ChannelInboundHandlerAdapter {
    private static final Logger logger = LoggerFactory.getLogger(BedrockLoginHandler.class);

    // RakNet offline message data (magic bytes appearing in every unconnected packet)
    private static final byte[] RAKNET_MAGIC = {
        (byte)0x00,(byte)0xFF,(byte)0xFF,(byte)0x00,
        (byte)0xFE,(byte)0xFE,(byte)0xFE,(byte)0xFE,
        (byte)0xFD,(byte)0xFD,(byte)0xFD,(byte)0xFD,
        (byte)0x12,(byte)0x34,(byte)0x56,(byte)0x78
    };

    /** Server GUID — any stable 8-byte identifier for this server instance. */
    private static final long SERVER_GUID = 0x4F56455244494E44L; // "OVERMIND" as big-endian

    private static final int MTU_SIZE = 1492;

    // Connection state machine
    private enum State { RAKNET_HANDSHAKE, RAKNET_CONNECTED, BEDROCK_LOGIN, IN_GAME }
    private State state = State.RAKNET_HANDSHAKE;

    private final RakNetReliabilityLayer reliability = new RakNetReliabilityLayer();
    private FragmentReassembler reassembler;

    private VertexGraphManager vertexGraphManager;
    private PlayerRegistry playerRegistry;
    private String playerName = "Unknown";
    private boolean compressionEnabled = false;

    // Shield/crouch adapter (wired in when we know the player name)
    private ShieldCrouchAdapter shieldAdapter;

    // Registry entry — set when the player enters IN_GAME state
    private PlayerRegistry.PlayerEntry myEntry;

    public BedrockLoginHandler() {}

    public void setVertexGraphManager(VertexGraphManager vgm) {
        this.vertexGraphManager = vgm;
    }

    public void setPlayerRegistry(PlayerRegistry registry) {
        this.playerRegistry = registry;
    }

    // ── Lifecycle ─────────────────────────────────────────────────────────────

    @Override
    public void channelActive(ChannelHandlerContext ctx) throws Exception {
        this.reassembler = new FragmentReassembler(ctx.alloc());
        logger.info("Bedrock client connected: {}", ctx.channel().remoteAddress());
    }

    @Override
    public void channelInactive(ChannelHandlerContext ctx) throws Exception {
        logger.info("Bedrock client disconnected: {}", ctx.channel().remoteAddress());
        if (playerRegistry != null && myEntry != null) {
            playerRegistry.unregister(playerName);
        }
        if (reassembler != null) reassembler.close();
        reliability.close();
    }

    // ── Inbound dispatch ─────────────────────────────────────────────────────

    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
        if (!(msg instanceof ByteBuf)) return;
        ByteBuf buf = (ByteBuf) msg;
        try {
            if (buf.readableBytes() < 1) return;
            int firstByte = buf.getByte(buf.readerIndex()) & 0xFF;

            // ── RakNet connectionless handshake packets ─────────────────────
            if (firstByte == PacketConstants.RAKNET_UNCONNECTED_PING ||
                firstByte == 0x02) {
                handleUnconnectedPing(ctx, buf);
            } else if (firstByte == PacketConstants.RAKNET_OPEN_CONNECTION_REQ_1) {
                handleOpenConnectionReq1(ctx, buf);
            } else if (firstByte == PacketConstants.RAKNET_OPEN_CONNECTION_REQ_2) {
                handleOpenConnectionReq2(ctx, buf);
            }
            // ── RakNet ACK / NAK ────────────────────────────────────────────
            else if (firstByte == PacketConstants.RAKNET_ACK) {
                buf.readByte(); // consume type
                reliability.processAckOrNak(buf, true, ctx);
            } else if (firstByte == PacketConstants.RAKNET_NAK) {
                buf.readByte();
                reliability.processAckOrNak(buf, false, ctx);
            }
            // ── RakNet DATA datagrams (0x80–0x8F) ──────────────────────────
            else if ((firstByte & 0xF0) == 0x80) {
                handleDataDatagram(ctx, buf);
            } else {
                logger.debug("Bedrock: unrecognised packet 0x{} from {}",
                        Integer.toHexString(firstByte), ctx.channel().remoteAddress());
            }
        } finally {
            buf.release();
        }
    }

    // ── RakNet handshake ─────────────────────────────────────────────────────

    private void handleUnconnectedPing(ChannelHandlerContext ctx, ByteBuf buf) throws Exception {
        buf.readByte(); // packet type
        long pingTime = buf.readLong();
        // magic (16 bytes) — skip
        buf.skipBytes(16);

        // Build server name string (MOTD)
        String motd = String.format("MCPE;Overmind;786;1.21.40;0;20;%d;Overmind Server;Survival;", SERVER_GUID);
        byte[] motdBytes = motd.getBytes(StandardCharsets.UTF_8);

        ByteBuf pong = ctx.alloc().buffer(35 + motdBytes.length);
        pong.writeByte(PacketConstants.RAKNET_UNCONNECTED_PONG);
        pong.writeLong(pingTime);
        pong.writeLong(SERVER_GUID);
        pong.writeBytes(RAKNET_MAGIC);
        pong.writeShort(motdBytes.length);
        pong.writeBytes(motdBytes);
        ctx.writeAndFlush(pong);
        logger.debug("Bedrock: UnconnectedPong sent");
    }

    private void handleOpenConnectionReq1(ChannelHandlerContext ctx, ByteBuf buf) throws Exception {
        buf.readByte(); // type
        buf.skipBytes(16); // magic
        buf.readByte();  // raknet protocol version
        // remaining bytes are null padding for MTU probing

        ByteBuf reply = ctx.alloc().buffer(28);
        reply.writeByte(PacketConstants.RAKNET_OPEN_CONNECTION_REPLY_1);
        reply.writeBytes(RAKNET_MAGIC);
        reply.writeLong(SERVER_GUID);
        reply.writeBoolean(false); // security
        reply.writeShort(MTU_SIZE);
        ctx.writeAndFlush(reply);
        logger.debug("Bedrock: OpenConnectionReply1 sent (MTU={})", MTU_SIZE);
    }

    private void handleOpenConnectionReq2(ChannelHandlerContext ctx, ByteBuf buf) throws Exception {
        buf.readByte(); // type
        buf.skipBytes(16); // magic
        // client's view of server address (variable length)
        skipAddress(buf);
        int clientMtu = buf.readShort() & 0xFFFF;
        long clientGuid = buf.readLong();

        ByteBuf reply = ctx.alloc().buffer(31);
        reply.writeByte(PacketConstants.RAKNET_OPEN_CONNECTION_REPLY_2);
        reply.writeBytes(RAKNET_MAGIC);
        reply.writeLong(SERVER_GUID);
        writeAddress(reply, ctx);   // server's public address
        reply.writeShort(clientMtu);
        reply.writeBoolean(false);  // encryption
        ctx.writeAndFlush(reply);
        state = State.RAKNET_CONNECTED;
        logger.info("Bedrock: RakNet handshake complete (client GUID=0x{}, MTU={})",
                Long.toHexString(clientGuid), clientMtu);
    }

    // ── RakNet DATA datagrams ────────────────────────────────────────────────

    private void handleDataDatagram(ChannelHandlerContext ctx, ByteBuf buf) throws Exception {
        buf.readByte(); // flags byte
        int seqNum = RakNetReliabilityLayer.readInt24LE(buf);
        reliability.onDatagramReceived(seqNum, ctx);

        // Parse encapsulated packets
        while (buf.readableBytes() > 0) {
            ByteBuf payload = readEncapsulatedPacket(ctx, buf);
            if (payload != null) {
                try {
                    handleGameEnvelope(ctx, payload);
                } finally {
                    payload.release();
                }
            }
        }
    }

    /**
     * Reads one encapsulated packet from the datagram.
     * Handles fragment reassembly.  Returns the payload buf (caller must release), or null.
     */
    private ByteBuf readEncapsulatedPacket(ChannelHandlerContext ctx, ByteBuf buf) throws Exception {
        if (buf.readableBytes() < 3) return null;

        byte reliabilityFlags = buf.readByte();
        int reliability_type  = (reliabilityFlags & 0xE0) >> 5;
        boolean isSplit       = (reliabilityFlags & 0x10) != 0;
        int bitLength         = buf.readShort() & 0xFFFF;
        int byteLength        = (bitLength + 7) / 8;

        // Reliable types include a message sequence number
        boolean isReliable = reliability_type == 2 || reliability_type == 3
                          || reliability_type == 4 || reliability_type == 6
                          || reliability_type == 7;
        if (isReliable) {
            RakNetReliabilityLayer.readInt24LE(buf); // message number (ignored for now)
        }
        // Ordered types include order index + channel
        boolean isOrdered = reliability_type == 3 || reliability_type == 7;
        if (isOrdered) {
            RakNetReliabilityLayer.readInt24LE(buf); // order index
            buf.readByte();                          // order channel
        }

        int splitCount  = 0;
        int compoundId  = 0;
        int splitIndex  = 0;
        if (isSplit) {
            splitCount = buf.readInt();
            compoundId = buf.readShort() & 0xFFFF;
            splitIndex = buf.readInt();
        }

        if (buf.readableBytes() < byteLength) {
            logger.warn("Bedrock: truncated encapsulated packet (need {}, have {})",
                    byteLength, buf.readableBytes());
            return null;
        }

        ByteBuf fragment = buf.readRetainedSlice(byteLength);

        if (isSplit && reassembler != null) {
            ByteBuf assembled = reassembler.add(compoundId, splitCount, splitIndex, fragment);
            fragment.release(); // reassembler retains its own copy
            return assembled;   // null if still incomplete
        }
        return fragment;
    }

    // ── Game packet envelope (0xFE batch / ConnectionRequest / NewIncomingConnection) ─

    private void handleGameEnvelope(ChannelHandlerContext ctx, ByteBuf payload) throws Exception {
        if (payload.readableBytes() < 1) return;
        int firstByte = payload.getByte(payload.readerIndex()) & 0xFF;

        if (firstByte == PacketConstants.RAKNET_CONNECTION_REQUEST) {
            handleConnectionRequest(ctx, payload);
        } else if (firstByte == PacketConstants.RAKNET_NEW_INCOMING_CONNECTION) {
            handleNewIncomingConnection(ctx);
        } else if (firstByte == PacketConstants.RAKNET_DISCONNECT) {
            logger.info("Bedrock: client sent disconnect");
            ctx.close();
        } else if (firstByte == 0xFE) {
            // Bedrock batch packet — contains one or more zlib-compressed game packets
            payload.readByte(); // consume 0xFE
            handleBatchPayload(ctx, payload);
        } else {
            logger.debug("Bedrock: unhandled game envelope 0x{}", Integer.toHexString(firstByte));
        }
    }

    private void handleConnectionRequest(ChannelHandlerContext ctx, ByteBuf payload) throws Exception {
        payload.readByte(); // type 0x09
        payload.readLong(); // clientGuid — not needed for offline-mode server
        long requestTime  = payload.readLong();
        // security byte follows — ignored

        long now = System.currentTimeMillis();
        ByteBuf accepted = ctx.alloc().buffer(96);
        accepted.writeByte(PacketConstants.RAKNET_CONNECTION_ACCEPTED);
        writeAddress(accepted, ctx);           // client's IP:port
        accepted.writeShort(0);               // system index
        for (int i = 0; i < 20; i++) writeAddress(accepted, ctx); // internal IDs (20 entries)
        accepted.writeLong(requestTime);
        accepted.writeLong(now);
        sendReliable(ctx, accepted);
        logger.info("Bedrock: ConnectionRequestAccepted sent");
    }

    private void handleNewIncomingConnection(ChannelHandlerContext ctx) {
        logger.info("Bedrock: NewIncomingConnection — RakNet fully connected");
        state = State.BEDROCK_LOGIN;
        // Wait for RequestNetworkSettings (0xC1) or Login (0x01) from client
    }

    // ── Bedrock game protocol ────────────────────────────────────────────────

    /**
     * Decompresses (if needed) and dispatches each game packet in a 0xFE batch.
     * Compression is enabled after NetworkSettings is sent with threshold > 0.
     */
    private void handleBatchPayload(ChannelHandlerContext ctx, ByteBuf compressed) throws Exception {
        ByteBuf data;
        if (compressionEnabled) {
            data = decompress(ctx.alloc(), compressed);
        } else {
            data = compressed.retain();
        }

        try {
            while (data.readableBytes() > 0) {
                int packetLen = readVarInt(data);
                if (data.readableBytes() < packetLen) break;
                ByteBuf packet = data.readRetainedSlice(packetLen);
                try {
                    dispatchGamePacket(ctx, packet);
                } finally {
                    packet.release();
                }
            }
        } finally {
            data.release();
        }
    }

    private void dispatchGamePacket(ChannelHandlerContext ctx, ByteBuf packet) throws Exception {
        if (packet.readableBytes() < 2) return;
        // Bedrock game packet ID is a VarInt header; lowest byte is the packet ID
        int header   = readVarInt(packet);
        int packetId = header & 0x3FF; // lower 10 bits are the packet ID

        switch (packetId) {
            case PacketConstants.BEDROCK_PKT_REQUEST_NET_SETTINGS:
                handleRequestNetworkSettings(ctx, packet);
                break;
            case PacketConstants.BEDROCK_PKT_LOGIN:
                handleBedrockLogin(ctx, packet);
                break;
            case PacketConstants.BEDROCK_PKT_RESOURCE_PACK_RESP:
                handleResourcePackResponse(ctx, packet);
                break;
            case PacketConstants.BEDROCK_PKT_REQUEST_CHUNK_RADIUS:
                handleRequestChunkRadius(ctx, packet);
                break;
            case PacketConstants.BEDROCK_PKT_PLAYER_ACTION:
                handlePlayerAction(ctx, packet);
                break;
            case PacketConstants.BEDROCK_PKT_MOVE_PLAYER:
                handleMovePlayer(ctx, packet);
                break;
            default:
                logger.debug("Bedrock: unhandled game packet 0x{}", Integer.toHexString(packetId));
                break;
        }
    }

    private void handleRequestNetworkSettings(ChannelHandlerContext ctx, ByteBuf payload) throws Exception {
        int clientProtocol = payload.readInt(); // BE int
        logger.info("Bedrock: RequestNetworkSettings — client protocol {}", clientProtocol);

        // Send NetworkSettings: compression threshold 1, algorithm ZLIB (0)
        ByteBuf ns = ctx.alloc().buffer(8);
        ns.writeByte(PacketConstants.BEDROCK_PKT_NETWORK_SETTINGS & 0xFF);
        ns.writeShortLE(1);    // compression threshold
        ns.writeShortLE(0);    // compression algorithm: ZLIB
        ns.writeBoolean(false); // client throttle
        ns.writeByte(0);       // throttle threshold
        ns.writeFloatLE(0f);   // throttle scalar
        sendBatchUncompressed(ctx, ns);
        compressionEnabled = true;
        logger.debug("Bedrock: NetworkSettings sent (ZLIB, threshold=1)");
    }

    private void handleBedrockLogin(ChannelHandlerContext ctx, ByteBuf payload) throws Exception {
        // Login body: int BE protocol version, then a blob with JWT chain + skin data.
        // For offline-mode we skip JWT validation and extract the display name from the chain.
        int protocol = payload.readInt();
        // Read the chain + skin blob (VarInt length prefixed)
        int blobLen = readVarInt(payload);
        byte[] blob = new byte[Math.min(blobLen, payload.readableBytes())];
        payload.readBytes(blob);

        // Extract "extraData.displayName" from the JWT payload using a simple scan
        playerName = extractDisplayName(new String(blob, StandardCharsets.UTF_8));
        shieldAdapter = new ShieldCrouchAdapter(playerName);

        logger.info("Bedrock: Login from '{}' (protocol {})", playerName, protocol);
        state = State.BEDROCK_LOGIN;

        // PlayStatus: 0 = LOGIN_SUCCESS
        sendGamePacket(ctx, buildPlayStatus(0));

        // ResourcePacksInfo: no packs
        sendGamePacket(ctx, buildResourcePacksInfo());
    }

    private void handleResourcePackResponse(ChannelHandlerContext ctx, ByteBuf payload) throws Exception {
        int status = payload.readByte() & 0xFF;
        logger.debug("Bedrock: ResourcePackClientResponse status={}", status);

        // Status 2 = SEND_PACKS (we have none), 3 = HAVE_ALL_PACKS, 4 = COMPLETED
        if (status == 2 || status == 3) {
            // Acknowledge with empty ResourcePackStack
            sendGamePacket(ctx, buildResourcePackStack());
        } else if (status == 4) {
            // Client is ready — send StartGame then chunks
            sendGamePacket(ctx, buildStartGame());
            sendSpawnChunks(ctx);
            state = State.IN_GAME;
            registerWithRegistry(ctx);
            logger.info("Bedrock: '{}' entered world", playerName);
        }
    }

    private void handleRequestChunkRadius(ChannelHandlerContext ctx, ByteBuf payload) throws Exception {
        int requested = readVarInt(payload);
        int granted   = Math.min(requested, vertexGraphManager != null
                ? vertexGraphManager.getViewDistance() : 8);

        ByteBuf resp = ctx.alloc().buffer(4);
        writeVarInt(resp, PacketConstants.BEDROCK_PKT_CHUNK_RADIUS_UPDATED);
        writeVarInt(resp, granted);
        sendGamePacket(ctx, resp);
        logger.debug("Bedrock: ChunkRadiusUpdated → {}", granted);
    }

    private void handlePlayerAction(ChannelHandlerContext ctx, ByteBuf payload) throws Exception {
        // VarInt entityRuntimeId, VarInt actionId, BlockCoordinate, int face
        readVarInt(payload); // entity runtime id
        int actionId = readVarInt(payload);
        if (shieldAdapter != null) shieldAdapter.onPlayerAction(actionId, ctx);
    }

    private void handleMovePlayer(ChannelHandlerContext ctx, ByteBuf payload) {
        if (state != State.IN_GAME) return;
        // VarLong runtimeEntityId (unsigned; read as VarInt since IDs are small)
        readVarInt(payload);
        float x       = payload.readFloatLE();
        float y       = payload.readFloatLE();
        float z       = payload.readFloatLE();
        float pitch   = payload.readFloatLE();
        float yaw     = payload.readFloatLE();
        float headYaw = payload.readFloatLE();
        // byte mode, bool onGround, VarLong ridingId follow — not needed for position sync
        if (playerRegistry != null && myEntry != null) {
            playerRegistry.updatePosition(myEntry, x, y, z, yaw, pitch, headYaw);
        }
    }

    // ── PlayerRegistry integration ───────────────────────────────────────────

    private void registerWithRegistry(ChannelHandlerContext ctx) {
        if (playerRegistry == null) return;
        myEntry = playerRegistry.register(playerName, true, 0.5, 100.0, 0.5,
                new PlayerRegistry.PlayerListener() {
                    @Override
                    public void onPlayerJoined(PlayerRegistry.PlayerEntry joiner) {
                        ctx.channel().eventLoop().execute(() -> {
                            try { sendAddPlayer(ctx, joiner); } catch (Exception e) {
                                logger.error("Bedrock: failed to spawn {} for {}",
                                        joiner.username, playerName, e);
                            }
                        });
                    }
                    @Override
                    public void onPlayerLeft(PlayerRegistry.PlayerEntry leaver) {
                        ctx.channel().eventLoop().execute(() -> {
                            try { sendRemoveEntity(ctx, leaver.entityId); } catch (Exception e) {
                                logger.error("Bedrock: failed to remove {} for {}",
                                        leaver.username, playerName, e);
                            }
                        });
                    }
                    @Override
                    public void onPlayerMoved(PlayerRegistry.PlayerEntry mover) {
                        ctx.channel().eventLoop().execute(() -> {
                            try { sendMovePlayerPacket(ctx, mover); } catch (Exception e) {
                                logger.warn("Bedrock: failed to move {} for {}",
                                        mover.username, playerName, e);
                            }
                        });
                    }
                });
    }

    // ── S→C Bedrock player visibility packets ────────────────────────────────

    /**
     * Sends AddPlayer (0x0C) to spawn {@code other} on this Bedrock client.
     *
     * <p>Fields follow the Bedrock 1.21.40 protocol: UUID, username, entity IDs,
     * position, rotation, held item (air), game type, empty metadata, and device info.
     */
    private void sendAddPlayer(ChannelHandlerContext ctx,
                               PlayerRegistry.PlayerEntry other) throws Exception {
        ByteBuf buf = createGamePacket(PacketConstants.BEDROCK_PKT_ADD_PLAYER);

        // UUID (16 bytes)
        UUID uuid = playerUuid(other.username);
        buf.writeLong(uuid.getMostSignificantBits());
        buf.writeLong(uuid.getLeastSignificantBits());

        writeString(buf, other.username);       // display name

        writeZigZagVarLong(buf, other.entityId); // entity unique id (signed, zigzag)
        writeVarInt(buf, other.entityId);        // entity runtime id (unsigned)

        writeString(buf, "");                   // platform chat id

        buf.writeFloatLE((float) other.x);      // position
        buf.writeFloatLE((float) other.y);
        buf.writeFloatLE((float) other.z);
        buf.writeFloatLE(0f);                   // velocity
        buf.writeFloatLE(0f);
        buf.writeFloatLE(0f);
        buf.writeFloatLE(other.pitch);          // rotation
        buf.writeFloatLE(other.yaw);
        buf.writeFloatLE(other.headYaw);

        writeVarInt(buf, 0);                    // held item: air (network ID 0)
        writeVarInt(buf, 0);                    // game type: 0 = survival

        // Entity metadata — 1 entry: FLAGS (key 0, type Long = 7, value 0)
        writeVarInt(buf, 1);
        writeVarInt(buf, 0);                    // key: FLAGS
        writeVarInt(buf, 7);                    // type: Long
        buf.writeLongLE(0L);                    // value: no flags

        writeVarInt(buf, 0);                    // entity property float count
        writeVarInt(buf, 0);                    // entity property int count
        writeVarInt(buf, 0);                    // entity links count

        writeString(buf, "");                   // device id
        buf.writeIntLE(-1);                     // build platform: -1 = UNKNOWN

        sendGamePacket(ctx, buf);
        logger.debug("Bedrock: AddPlayer '{}' sent to '{}'", other.username, playerName);
    }

    /** Sends RemoveEntity (0x0E) to despawn an entity by runtime ID. */
    private void sendRemoveEntity(ChannelHandlerContext ctx, int entityId) throws Exception {
        ByteBuf buf = createGamePacket(PacketConstants.BEDROCK_PKT_REMOVE_ENTITY);
        writeZigZagVarLong(buf, entityId);      // entity unique id (signed, zigzag)
        sendGamePacket(ctx, buf);
    }

    /**
     * Sends MovePlayer (0x13) to move {@code mover} on this Bedrock client.
     * Mode 0 = NORMAL (smooth client-side interpolation).
     */
    private void sendMovePlayerPacket(ChannelHandlerContext ctx,
                                      PlayerRegistry.PlayerEntry mover) throws Exception {
        ByteBuf buf = createGamePacket(PacketConstants.BEDROCK_PKT_MOVE_PLAYER);
        writeVarInt(buf, mover.entityId);       // runtime entity id
        buf.writeFloatLE((float) mover.x);
        buf.writeFloatLE((float) mover.y);
        buf.writeFloatLE((float) mover.z);
        buf.writeFloatLE(mover.pitch);
        buf.writeFloatLE(mover.yaw);
        buf.writeFloatLE(mover.headYaw);
        buf.writeByte(0);                       // mode: 0 = NORMAL
        buf.writeBoolean(true);                 // on ground
        writeVarInt(buf, 0);                    // riding runtime entity id (none)
        sendGamePacket(ctx, buf);
    }

    // ── Zigzag VarLong helper ─────────────────────────────────────────────────

    private void writeZigZagVarLong(ByteBuf buf, long value) {
        long encoded = (value << 1) ^ (value >> 63);
        while ((encoded & 0xFFFFFFFFFFFFFF80L) != 0) {
            buf.writeByte((byte) ((encoded & 0x7F) | 0x80));
            encoded >>>= 7;
        }
        buf.writeByte((byte) (encoded & 0x7F));
    }

    // ── UUID helper ───────────────────────────────────────────────────────────

    private static UUID playerUuid(String username) {
        return UUID.nameUUIDFromBytes(
                ("OfflinePlayer:" + username).getBytes(StandardCharsets.UTF_8));
    }

    // ── Chunk sending ────────────────────────────────────────────────────────

    private void sendSpawnChunks(ChannelHandlerContext ctx) throws Exception {
        if (vertexGraphManager == null) return;
        int r = vertexGraphManager.getViewDistance();
        vertexGraphManager.loadArea(0, 0, r);
        for (int cx = -r; cx <= r; cx++) {
            for (int cz = -r; cz <= r; cz++) {
                ChunkNode chunk = vertexGraphManager.getChunk(cx, cz);
                ByteBuf chunkPacket = ChunkPacketBuilder.buildBedrockChunkPacket(
                        ctx.alloc(), cx, cz, chunk);
                // Wrap in game-packet VarInt header
                ByteBuf wrapped = wrapGamePacket(ctx.alloc(),
                        PacketConstants.BEDROCK_PKT_LEVEL_CHUNK, chunkPacket);
                chunkPacket.release();
                sendGamePacket(ctx, wrapped);
            }
        }
        logger.info("Bedrock: spawn chunks sent to '{}'", playerName);
    }

    // ── Packet builders ──────────────────────────────────────────────────────

    private ByteBuf buildPlayStatus(int status) {
        ByteBuf buf = createGamePacket(PacketConstants.BEDROCK_PKT_PLAY_STATUS);
        buf.writeInt(status); // BE int
        return buf;
    }

    private ByteBuf buildResourcePacksInfo() {
        ByteBuf buf = createGamePacket(PacketConstants.BEDROCK_PKT_RESOURCE_PACKS_INFO);
        buf.writeBoolean(false); // must_accept
        buf.writeBoolean(false); // scripting
        buf.writeBoolean(false); // forcing_server_packs
        buf.writeShortLE(0);     // behaviour_packs count
        buf.writeShortLE(0);     // resource_packs count
        return buf;
    }

    private ByteBuf buildResourcePackStack() {
        ByteBuf buf = createGamePacket(PacketConstants.BEDROCK_PKT_RESOURCE_PACK_STACK);
        buf.writeBoolean(false); // must_accept
        writeVarInt(buf, 0);     // addon_packs count
        writeVarInt(buf, 0);     // resource_packs count
        writeString(buf, "1.21.40"); // game_version
        writeVarInt(buf, 0);     // experiments count
        buf.writeBoolean(false); // experiments_previously_toggled
        return buf;
    }

    private ByteBuf buildStartGame() {
        ByteBuf buf = createGamePacket(PacketConstants.BEDROCK_PKT_START_GAME);
        // Entity runtime ID and unique ID
        writeVarInt(buf, 1);      // entity unique id (zigzag VarInt64)
        writeVarInt(buf, 1);      // entity runtime id
        writeVarInt(buf, 0);      // player gamemode: 0 = SURVIVAL
        // Spawn position (float BE)
        buf.writeFloatLE(0.5f);   // x
        buf.writeFloatLE(100.0f); // y
        buf.writeFloatLE(0.5f);   // z
        buf.writeFloatLE(0);      // pitch
        buf.writeFloatLE(0);      // yaw
        // World seed
        buf.writeLongLE(12345L);
        buf.writeShortLE(0);      // biome type: 0 = default
        writeString(buf, "plains"); // biome name
        writeVarInt(buf, 0);      // dimension: 0 = OVERWORLD
        writeVarInt(buf, 1);      // generator: 1 = infinite
        writeVarInt(buf, 0);      // world gamemode: 0 = SURVIVAL
        buf.writeBoolean(false);  // hardcore
        writeVarInt(buf, 0);      // difficulty: 0 = PEACEFUL
        // Default spawn position (block coordinates)
        writeVarInt(buf, 0);      // x
        writeVarInt(buf, 100);    // y
        writeVarInt(buf, 0);      // z
        buf.writeBoolean(false);  // has achievements disabled
        buf.writeBoolean(false);  // editor world
        buf.writeBoolean(false);  // created in editor
        buf.writeBoolean(false);  // exported from editor
        writeVarInt(buf, 0);      // day cycle stop time
        writeVarInt(buf, 0);      // edu edition offers
        buf.writeBoolean(false);  // edu features
        writeString(buf, "");     // edu product id
        buf.writeFloatLE(1.0f);   // rain level
        buf.writeFloatLE(0.0f);   // lightning level
        buf.writeBoolean(false);  // confirmed platform locked content
        buf.writeBoolean(true);   // multiplayer game
        buf.writeBoolean(true);   // LAN broadcast
        writeVarInt(buf, 4);      // XBL broadcast intent: 4 = PUBLIC
        writeVarInt(buf, 4);      // platform broadcast intent: 4 = PUBLIC
        buf.writeBoolean(false);  // commands enabled (false = disabled from client)
        buf.writeBoolean(false);  // texture packs required
        writeVarInt(buf, 0);      // game rules count
        writeVarInt(buf, 0);      // experiments count
        buf.writeBoolean(false);  // bonus chest enabled
        buf.writeBoolean(false);  // start with map
        writeVarInt(buf, 1);      // player permission: 1 = MEMBER
        buf.writeIntLE(0);        // server chunk tick range
        buf.writeBoolean(false);  // has locked behaviour packs
        buf.writeBoolean(false);  // has locked resource packs
        buf.writeBoolean(false);  // is from locked world template
        buf.writeBoolean(false);  // use MSA gamertag-only
        buf.writeBoolean(false);  // from world template
        buf.writeBoolean(false);  // world template option locked
        buf.writeBoolean(false);  // only spawn v1 villagers
        buf.writeBoolean(false);  // disable persona
        buf.writeBoolean(false);  // disable custom skins
        buf.writeBoolean(false);  // mute emote chat
        writeString(buf, "1.21.40"); // game version
        buf.writeIntLE(20);       // limited world width
        buf.writeIntLE(20);       // limited world height
        buf.writeBoolean(false);  // new nether
        writeString(buf, "");     // edu shared resource uri base
        writeString(buf, "");     // edu shared resource uri button name
        buf.writeBoolean(false);  // force experimental gameplay
        buf.writeByte(1);         // chat restriction: 1 = NONE
        buf.writeBoolean(false);  // disable player interactions
        writeString(buf, "Overmind"); // level id
        writeString(buf, "Overmind"); // world name
        writeString(buf, "");     // premium world template id
        buf.writeBoolean(false);  // is trial
        writeVarInt(buf, 0);      // movement auth: 0 = CLIENT
        buf.writeBoolean(false);  // rewind history
        buf.writeBoolean(false);  // server authoritative block breaking
        buf.writeLongLE(0L);      // current tick
        writeVarInt(buf, 0);      // enchantment seed
        writeVarInt(buf, 0);      // custom blocks count
        writeVarInt(buf, 0);      // items count
        writeString(buf, "");     // multiplayer correlation id
        buf.writeBoolean(false);  // server authoritative inventory
        writeString(buf, "Overmind 1.0"); // engine
        // property data NBT (empty compound)
        buf.writeByte(0x0A);      // TAG_Compound
        buf.writeShort(0);        // empty name
        buf.writeByte(0x00);      // TAG_End
        buf.writeLongLE(0L);      // block palette checksum
        writeString(buf, "");     // world template id
        buf.writeBoolean(false);  // client side generation
        buf.writeBoolean(false);  // use block network id hashes
        buf.writeBoolean(false);  // server controlled sound
        return buf;
    }

    // ── Send helpers ─────────────────────────────────────────────────────────

    /** Sends a game packet wrapped in a zlib-compressed 0xFE batch. */
    private void sendGamePacket(ChannelHandlerContext ctx, ByteBuf gamePacket) throws Exception {
        ByteBuf batch = compressBatch(ctx.alloc(), gamePacket);
        gamePacket.release();
        sendReliable(ctx, batch);
    }

    /** Sends a game packet UNCOMPRESSED (used for NetworkSettings, before compression kicks in). */
    private void sendBatchUncompressed(ChannelHandlerContext ctx, ByteBuf gamePacket) throws Exception {
        ByteBuf batch = ctx.alloc().buffer(3 + gamePacket.readableBytes());
        batch.writeByte(0xFE); // batch wrapper
        // VarInt length of the game packet (no compression)
        writeVarInt(batch, gamePacket.readableBytes());
        batch.writeBytes(gamePacket);
        gamePacket.release();
        sendReliable(ctx, batch);
    }

    /** Wraps payload in a RELIABLE_ORDERED datagram and sends it. */
    private void sendReliable(ChannelHandlerContext ctx, ByteBuf payload) {
        ByteBuf datagram = reliability.buildReliableOrderedDatagram(ctx.alloc(), payload);
        payload.release();
        ctx.writeAndFlush(datagram);
    }

    /** Compresses game packets into a 0xFE batch using zlib. */
    private ByteBuf compressBatch(ByteBufAllocator alloc, ByteBuf gamePacket) {
        byte[] input = new byte[gamePacket.readableBytes()];
        gamePacket.getBytes(gamePacket.readerIndex(), input);

        Deflater deflater = new Deflater(Deflater.DEFAULT_COMPRESSION, false);
        deflater.setInput(input);
        deflater.finish();
        byte[] compressed = new byte[input.length + 64];
        int len = deflater.deflate(compressed);
        deflater.end();

        ByteBuf batch = alloc.buffer(1 + 4 + len);
        batch.writeByte(0xFE); // batch type
        writeVarInt(batch, len + 2); // length of length-prefixed game packet
        writeVarInt(batch, input.length); // uncompressed game packet length varint
        batch.writeBytes(compressed, 0, len);
        return batch;
    }

    private ByteBuf decompress(ByteBufAllocator alloc, ByteBuf compressed) {
        byte[] input = new byte[compressed.readableBytes()];
        compressed.readBytes(input);
        try {
            java.util.zip.Inflater inf = new java.util.zip.Inflater(false);
            inf.setInput(input);
            byte[] out = new byte[input.length * 4 + 256];
            int len = inf.inflate(out);
            inf.end();
            ByteBuf result = alloc.buffer(len);
            result.writeBytes(out, 0, len);
            return result;
        } catch (Exception e) {
            logger.warn("Bedrock: decompression failed: {}", e.getMessage());
            return alloc.buffer(0);
        }
    }

    // ── Utility ──────────────────────────────────────────────────────────────

    private ByteBuf createGamePacket(int packetId) {
        ByteBuf buf = io.netty.buffer.Unpooled.buffer(16);
        writeVarInt(buf, packetId);
        return buf;
    }

    private ByteBuf wrapGamePacket(ByteBufAllocator alloc, int packetId, ByteBuf body) {
        ByteBuf out = alloc.buffer(4 + body.readableBytes());
        writeVarInt(out, packetId);
        out.writeBytes(body);
        return out;
    }

    private void writeAddress(ByteBuf buf, ChannelHandlerContext ctx) {
        // IPv4 address: type 4, address bytes, port
        buf.writeByte(4); // AF_INET
        buf.writeByte(127 ^ 0xFF); // 127.0.0.1 XOR 0xFF per RakNet spec
        buf.writeByte(0 ^ 0xFF);
        buf.writeByte(0 ^ 0xFF);
        buf.writeByte(1 ^ 0xFF);
        buf.writeShort(25565);
    }

    private void skipAddress(ByteBuf buf) {
        byte type = buf.readByte();
        if (type == 4) {
            buf.skipBytes(4 + 2); // 4 address bytes + 2 port bytes
        } else if (type == 6) {
            buf.skipBytes(2 + 16 + 4 + 4); // family + address + port + scope_id (BE)
        }
    }

    private String extractDisplayName(String json) {
        // Simple extraction of "displayName" value from the Login JWT chain blob
        int idx = json.indexOf("\"displayName\"");
        if (idx < 0) return "BedrockPlayer";
        int colon = json.indexOf(':', idx);
        if (colon < 0) return "BedrockPlayer";
        int start = json.indexOf('"', colon + 1);
        int end   = json.indexOf('"', start + 1);
        if (start < 0 || end < 0) return "BedrockPlayer";
        return json.substring(start + 1, end);
    }

    private void writeString(ByteBuf buf, String s) {
        byte[] bytes = s.getBytes(StandardCharsets.UTF_8);
        writeVarInt(buf, bytes.length);
        buf.writeBytes(bytes);
    }

    private void writeVarInt(ByteBuf buf, int v) {
        while (true) {
            if ((v & 0xFFFFFF80) == 0) { buf.writeByte(v); return; }
            buf.writeByte((v & 0x7F) | 0x80);
            v >>>= 7;
        }
    }

    private int readVarInt(ByteBuf buf) {
        int value = 0, position = 0;
        byte b;
        do {
            b = buf.readByte();
            value |= (b & 0x7F) << position;
            position += 7;
        } while ((b & 0x80) != 0 && position < 35);
        return value;
    }

    /** Sends a chunk to this Bedrock client. Called externally when the world updates. */
    public void sendChunkToBedrockClient(ChannelHandlerContext ctx, int chunkX, int chunkZ) {
        try {
            ChunkNode chunk = vertexGraphManager != null
                    ? vertexGraphManager.getChunk(chunkX, chunkZ) : null;
            ByteBuf chunkPacket = ChunkPacketBuilder.buildBedrockChunkPacket(
                    ctx.alloc(), chunkX, chunkZ, chunk);
            ByteBuf wrapped = wrapGamePacket(ctx.alloc(), PacketConstants.BEDROCK_PKT_LEVEL_CHUNK, chunkPacket);
            chunkPacket.release();
            sendGamePacket(ctx, wrapped);
        } catch (Exception e) {
            logger.error("Bedrock: failed to send chunk ({}, {})", chunkX, chunkZ, e);
        }
    }

    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
        logger.error("Exception in Bedrock handler for '{}'", playerName, cause);
        ctx.close();
    }
}
