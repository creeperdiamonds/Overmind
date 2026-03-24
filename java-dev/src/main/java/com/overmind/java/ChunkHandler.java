package com.overmind.java;

import com.overmind.api.ChunkNode;
import com.overmind.api.VertexGraphManager;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelHandlerContext;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Utility for encoding and dispatching chunk packets to connected clients.
 * This is not a Netty pipeline handler — it is used as a helper by {@link PlayHandler}.
 */
public class ChunkHandler {
    private static final Logger logger = LoggerFactory.getLogger(ChunkHandler.class);

    private final VertexGraphManager vertexGraphManager;

    public ChunkHandler(VertexGraphManager vertexGraphManager) {
        this.vertexGraphManager = vertexGraphManager;
    }

    /**
     * Encodes and sends a Chunk Data and Update Light packet (0x27) to a Java 1.21.4 client.
     * In OVERMIND mode the fake-air layer at y=383 is included via {@link ChunkNode#getBlockForJavaClient}.
     */
    public void sendChunkToJavaClient(ChannelHandlerContext ctx, int chunkX, int chunkZ) {
        ChunkNode chunk = vertexGraphManager.getChunk(chunkX, chunkZ);
        ByteBuf packet = ChunkPacketBuilder.buildJavaChunkPacket(ctx.alloc(), chunkX, chunkZ, chunk);
        logger.debug("Sending Java chunk ({}, {}): {} bytes", chunkX, chunkZ, packet.readableBytes());
        ctx.writeAndFlush(packet);
    }
}
