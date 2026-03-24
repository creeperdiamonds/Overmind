package com.overmind.java;

import com.overmind.api.VertexGraphManager;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.handler.codec.ByteToMessageDecoder;
import io.netty.handler.codec.http.HttpServerCodec;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.nio.charset.StandardCharsets;
import java.util.List;

public class ProtocolDetector extends ByteToMessageDecoder {
    private static final Logger logger = LoggerFactory.getLogger(ProtocolDetector.class);

    private static final int HTTP_GET_MAGIC     = 0x47455420; // "GET "
    private static final int HTTP_POST_MAGIC    = 0x504F5354; // "POST"
    private static final int HTTP_PUT_MAGIC     = 0x50555420; // "PUT "
    private static final int HTTP_DELETE_MAGIC  = 0x44454C45; // "DELE"
    private static final int HTTP_HEAD_MAGIC    = 0x48454144; // "HEAD"
    private static final int HTTP_OPTIONS_MAGIC = 0x4F505449; // "OPTI"
    private static final int HTTP_PATCH_MAGIC   = 0x50415443; // "PATC"

    private static final int MINECRAFT_HANDSHAKE_MAGIC = 0x00;

    // RakNet offline message data ID — appears in every unconnected handshake packet
    private static final byte[] RAKNET_MAGIC = {
        (byte)0x00, (byte)0xFF, (byte)0xFF, (byte)0x00,
        (byte)0xFE, (byte)0xFE, (byte)0xFE, (byte)0xFE,
        (byte)0xFD, (byte)0xFD, (byte)0xFD, (byte)0xFD,
        (byte)0x12, (byte)0x34, (byte)0x56, (byte)0x78
    };

    // Minimum bytes needed to confirm each RakNet packet type
    // 0x01 / 0x02 UnconnectedPing:        1 (id) + 8 (time) + 16 (magic) = 25
    // 0x05 OpenConnectionRequest1:        1 (id) + 16 (magic)             = 17
    private static final int RAKNET_PING_MIN_BYTES = 25;
    private static final int RAKNET_OPEN_MIN_BYTES = 17;

    private VertexGraphManager vertexGraphManager;
    private PlayerRegistry playerRegistry;

    @Override
    protected void decode(ChannelHandlerContext ctx, ByteBuf in, List<Object> out) throws Exception {
        if (in.readableBytes() < 4) {
            return;
        }

        in.markReaderIndex();
        int firstInt = in.readInt();

        // 1. HTTP
        if (isHttpRequest(firstInt)) {
            logger.debug("Detected HTTP request from {}", ctx.channel().remoteAddress());
            in.resetReaderIndex();
            ctx.pipeline().addLast("httpCodec", new HttpServerCodec());
            ctx.pipeline().addLast("httpHandler", new HttpServerHandler());
            ctx.pipeline().remove(this);
            return;
        }

        // 2. Bedrock RakNet — peek first byte to know how many bytes are needed
        byte firstByte = (byte) (firstInt >>> 24);
        if (firstByte == 0x01 || firstByte == 0x02 || firstByte == 0x05) {
            int needed = (firstByte == 0x05) ? RAKNET_OPEN_MIN_BYTES : RAKNET_PING_MIN_BYTES;
            in.resetReaderIndex();
            if (in.readableBytes() < needed) {
                return; // wait for more data before deciding
            }
            if (isRakNetHandshake(in)) {
                logger.debug("Detected Bedrock RakNet handshake from {}", ctx.channel().remoteAddress());
                in.resetReaderIndex();
                BedrockLoginHandler bedrockHandler = new BedrockLoginHandler();
                bedrockHandler.setVertexGraphManager(vertexGraphManager);
                bedrockHandler.setPlayerRegistry(playerRegistry);
                ctx.pipeline().addLast("bedrockHandler", bedrockHandler);
                ctx.pipeline().remove(this);
                return;
            }
        }

        // 3. Minecraft Java handshake
        in.resetReaderIndex();
        if (isMinecraftHandshake(in)) {
            logger.debug("Detected Minecraft Java handshake from {}", ctx.channel().remoteAddress());
            in.resetReaderIndex();
            MinecraftLoginHandler minecraftHandler = new MinecraftLoginHandler();
            minecraftHandler.setVertexGraphManager(vertexGraphManager);
            minecraftHandler.setPlayerRegistry(playerRegistry);
            ctx.pipeline().addLast("minecraftHandler", minecraftHandler);
            ctx.pipeline().remove(this);
            return;
        }

        // 4. Unknown protocol — send a human-readable rejection then close
        logger.warn("Unknown protocol from {}, rejecting gracefully", ctx.channel().remoteAddress());
        in.resetReaderIndex();
        sendGracefulRejection(ctx);
    }

    private boolean isHttpRequest(int firstInt) {
        return firstInt == HTTP_GET_MAGIC    ||
               firstInt == HTTP_POST_MAGIC   ||
               firstInt == HTTP_PUT_MAGIC    ||
               firstInt == HTTP_DELETE_MAGIC ||
               firstInt == HTTP_HEAD_MAGIC   ||
               firstInt == HTTP_OPTIONS_MAGIC||
               firstInt == HTTP_PATCH_MAGIC;
    }

    /**
     * Checks bytes at {@code in.readerIndex()} without advancing the reader index.
     * Caller must ensure the buffer has at least (magicOffset + 16) bytes readable
     * from readerIndex before calling.
     */
    private boolean isRakNetHandshake(ByteBuf in) {
        byte firstByte = in.getByte(in.readerIndex());
        int magicOffset;
        if (firstByte == 0x01 || firstByte == 0x02) {
            // UnconnectedPing: 1-byte id + 8-byte timestamp, magic follows at offset 9
            magicOffset = in.readerIndex() + 9;
        } else if (firstByte == 0x05) {
            // OpenConnectionRequest1: magic immediately after the 1-byte id
            magicOffset = in.readerIndex() + 1;
        } else {
            return false;
        }
        for (int i = 0; i < RAKNET_MAGIC.length; i++) {
            if (in.getByte(magicOffset + i) != RAKNET_MAGIC[i]) {
                return false;
            }
        }
        return true;
    }

    private boolean isMinecraftHandshake(ByteBuf in) {
        if (in.readableBytes() < 2) {
            return false;
        }
        int packetLength = readVarInt(in);
        if (packetLength < 1 || packetLength > 0x3FFFFFFF) {
            return false;
        }
        if (in.readableBytes() < 1) {
            return false;
        }
        int packetId = readVarInt(in);
        return packetId == MINECRAFT_HANDSHAKE_MAGIC;
    }

    private void sendGracefulRejection(ChannelHandlerContext ctx) {
        String msg = "Unknown protocol.\n" +
                     "This is an Overmind server. Supported clients:\n" +
                     "  - Minecraft Java Edition\n" +
                     "  - Minecraft Bedrock Edition\n" +
                     "  - HTTP browser (port 25565)\n";
        ByteBuf buf = ctx.alloc().buffer();
        buf.writeBytes(msg.getBytes(StandardCharsets.UTF_8));
        ctx.writeAndFlush(buf).addListener(ChannelFutureListener.CLOSE);
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

    public void setVertexGraphManager(VertexGraphManager vertexGraphManager) {
        this.vertexGraphManager = vertexGraphManager;
    }

    public void setPlayerRegistry(PlayerRegistry playerRegistry) {
        this.playerRegistry = playerRegistry;
    }
}
