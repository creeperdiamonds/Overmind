package com.overmind.java;

import com.overmind.api.VertexGraphManager;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.nio.charset.StandardCharsets;

public class LoginHandler extends ChannelInboundHandlerAdapter {
    private static final Logger logger = LoggerFactory.getLogger(LoginHandler.class);

    private final VertexGraphManager vertexGraphManager;
    private final PlayerRegistry playerRegistry;
    private String username = "unknown";

    public LoginHandler(VertexGraphManager vertexGraphManager, PlayerRegistry playerRegistry) {
        this.vertexGraphManager = vertexGraphManager;
        this.playerRegistry = playerRegistry;
    }

    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
        if (msg instanceof ByteBuf) {
            ByteBuf buf = (ByteBuf) msg;
            try {
                readVarInt(buf); // consume length prefix
                int packetId = readVarInt(buf);

                switch (packetId) {
                    case 0x00: // Login Start
                        username = readString(buf);
                        // Protocol 764+ (1.20.2+): Login Start also carries the player UUID (16 bytes).
                        // Skip it — we use offline-mode zero UUIDs and don't need the client-supplied value.
                        if (buf.readableBytes() >= 16) {
                            buf.skipBytes(16); // UUID most-significant + least-significant longs
                        }
                        logger.info("Login request from user: {}", username);
                        sendLoginSuccess(ctx, username);
                        break;

                    case 0x03: // Login Acknowledged (1.20.2+)
                        // Client has entered Configuration state.
                        logger.info("Login acknowledged by {}, entering Configuration state", username);
                        ctx.pipeline().addLast("configHandler",
                                new ConfigurationHandler(username, vertexGraphManager, playerRegistry));
                        ctx.pipeline().remove(this);
                        break;

                    default:
                        logger.warn("Unknown login packet ID: 0x{}", Integer.toHexString(packetId));
                        break;
                }
            } finally {
                buf.release();
            }
        }
    }

    // -------------------------------------------------------------------------
    // Outbound packets
    // -------------------------------------------------------------------------

    /**
     * Login Success (0x02) — protocol 766+ (1.20.6+) format:
     *   UUID                  16 bytes (two big-endian longs)
     *   Username              String
     *   Number of properties  VarInt  (0 = offline mode, no skin properties)
     *
     * Note: the "Strict Error Handling" boolean was present in 1.20.3–1.20.5 (protocols 765–765)
     * but was removed in 1.20.6 (protocol 766). Protocol 774 does not have this field.
     */
    private void sendLoginSuccess(ChannelHandlerContext ctx, String username) throws Exception {
        ByteBuf response = ctx.alloc().buffer();
        writeVarInt(response, 0x02);    // Packet ID: Login Success

        response.writeLong(0L);         // UUID most-significant bits  (offline = all zeros)
        response.writeLong(0L);         // UUID least-significant bits

        writeString(response, username);
        writeVarInt(response, 0);       // properties count (offline mode → none)

        ctx.writeAndFlush(frame(ctx, response));
        logger.info("Login Success sent to {}", username);
        // ConfigurationHandler is added when the client sends Login Acknowledged (0x03).
    }

    // -------------------------------------------------------------------------
    // Helpers
    // -------------------------------------------------------------------------

    /** Wraps a payload buffer with a VarInt length prefix. Releases payload. */
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
            if ((currentByte & 0x80) == 0) {
                break;
            }
            position += 7;
        } while (true);
        return value;
    }

    private String readString(ByteBuf buf) throws Exception {
        int length = readVarInt(buf);
        byte[] bytes = new byte[length];
        buf.readBytes(bytes);
        return new String(bytes, StandardCharsets.UTF_8);
    }

    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
        logger.error("Exception in Login handler", cause);
        ctx.close();
    }
}
