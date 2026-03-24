package com.overmind.java;

import com.overmind.api.VertexGraphManager;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.nio.charset.StandardCharsets;

/**
 * Handles the Minecraft 1.20.2+ Configuration state.
 *
 * <p>Flow:
 * <ol>
 *   <li>handlerAdded → send Clientbound Known Packs (0x0E) listing minecraft:core/1.21.4
 *   <li>Receive Serverbound Known Packs (0x07) → send Finish Configuration (0x03)
 *   <li>Receive Acknowledge Finish Configuration (0x03) → add PlayHandler, remove self
 * </ol>
 */
public class ConfigurationHandler extends ChannelInboundHandlerAdapter {
    private static final Logger logger = LoggerFactory.getLogger(ConfigurationHandler.class);

    private final String username;
    private final VertexGraphManager vertexGraphManager;
    private final PlayerRegistry playerRegistry;

    public ConfigurationHandler(String username, VertexGraphManager vertexGraphManager,
                                 PlayerRegistry playerRegistry) {
        this.username = username;
        this.vertexGraphManager = vertexGraphManager;
        this.playerRegistry = playerRegistry;
    }

    // -------------------------------------------------------------------------
    // Lifecycle — fire Known Packs as soon as we're in the pipeline
    // -------------------------------------------------------------------------

    @Override
    public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
        logger.info("Configuration state entered for {}", username);
        sendKnownPacks(ctx);
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
            logger.debug("Configuration packet: length={}, id=0x{}", packetLength, Integer.toHexString(packetId));

            switch (packetId) {
                case PacketConstants.CONFIG_SERVERBOUND_KNOWN_PACKS:
                    // Client tells us which packs it knows. We don't need to inspect them —
                    // just acknowledge configuration is finished.
                    logger.info("Serverbound Known Packs received from {}", username);
                    sendFinishConfiguration(ctx);
                    break;

                case PacketConstants.CONFIG_SERVERBOUND_ACK_FINISH:
                    // Client has entered Play state.
                    logger.info("Acknowledge Finish Configuration received from {}", username);
                    ctx.pipeline().addLast("playHandler", new PlayHandler(username, vertexGraphManager, playerRegistry));
                    ctx.pipeline().remove(this);
                    break;

                default:
                    logger.debug("Unhandled configuration packet 0x{} from {}",
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
     * Clientbound Known Packs (0x0E) — advertise minecraft:core version 1.21.4.
     *
     * <pre>
     * VarInt  known_pack_count   (1)
     * String  namespace          "minecraft"
     * String  id                 "core"
     * String  version            "1.21.4"
     * </pre>
     */
    private void sendKnownPacks(ChannelHandlerContext ctx) throws Exception {
        ByteBuf payload = ctx.alloc().buffer();
        writeVarInt(payload, PacketConstants.CONFIG_CLIENTBOUND_KNOWN_PACKS);
        writeVarInt(payload, 1);               // 1 pack
        writeString(payload, "minecraft");     // namespace
        writeString(payload, "core");          // id
        writeString(payload, "1.21.4");        // version
        ctx.writeAndFlush(frame(ctx, payload));
        logger.info("Clientbound Known Packs sent to {}", username);
    }

    /**
     * Clientbound Finish Configuration (0x03) — signals the end of configuration.
     * No body fields.
     */
    private void sendFinishConfiguration(ChannelHandlerContext ctx) throws Exception {
        ByteBuf payload = ctx.alloc().buffer();
        writeVarInt(payload, PacketConstants.CONFIG_CLIENTBOUND_FINISH);
        ctx.writeAndFlush(frame(ctx, payload));
        logger.info("Finish Configuration sent to {}", username);
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
        byte[] bytes = str.getBytes(StandardCharsets.UTF_8);
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
        logger.error("Exception in Configuration handler for {}", username, cause);
        ctx.close();
    }
}
