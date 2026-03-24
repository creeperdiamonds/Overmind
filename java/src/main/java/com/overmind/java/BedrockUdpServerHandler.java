package com.overmind.java;

import com.overmind.api.VertexGraphManager;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.SimpleChannelInboundHandler;
import io.netty.channel.socket.DatagramPacket;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.net.InetSocketAddress;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * UDP handler for native Minecraft Bedrock Edition clients (port 19132).
 *
 * <p>Each {@link DatagramPacket} arriving on the UDP channel carries a source address. Because
 * UDP is connectionless, we maintain a per-address {@link BedrockUdpSession} that owns the
 * {@link RakNetReliabilityLayer} and {@link FragmentReassembler} for that client.
 *
 * <p>The session holds a {@link BedrockLoginHandler} and delegates all packet processing to it.
 * Replies are sent back to the client's address using
 * {@code ctx.writeAndFlush(new DatagramPacket(buf, clientAddress))}.
 */
public class BedrockUdpServerHandler extends SimpleChannelInboundHandler<DatagramPacket> {
    private static final Logger logger = LoggerFactory.getLogger(BedrockUdpServerHandler.class);

    // Maximum number of concurrent Bedrock sessions
    private static final int MAX_SESSIONS = 256;

    private final VertexGraphManager vertexGraphManager;
    private final PlayerRegistry playerRegistry;
    private final Map<InetSocketAddress, BedrockUdpSession> sessions = new ConcurrentHashMap<>();

    public BedrockUdpServerHandler(VertexGraphManager vertexGraphManager, PlayerRegistry playerRegistry) {
        this.vertexGraphManager = vertexGraphManager;
        this.playerRegistry = playerRegistry;
    }

    @Override
    protected void channelRead0(ChannelHandlerContext ctx, DatagramPacket packet) throws Exception {
        InetSocketAddress sender = packet.sender();
        ByteBuf content = packet.content();

        if (content.readableBytes() < 1) return;

        BedrockUdpSession session = sessions.computeIfAbsent(sender, addr -> {
            if (sessions.size() >= MAX_SESSIONS) {
                logger.warn("BedrockUdpServerHandler: MAX_SESSIONS ({}) reached, rejecting {}", MAX_SESSIONS, addr);
                return null;
            }
            logger.info("Bedrock UDP: new session from {}", addr);
            BedrockUdpSession s = new BedrockUdpSession(ctx, addr);
            BedrockLoginHandler handler = new BedrockLoginHandler();
            handler.setVertexGraphManager(vertexGraphManager);
            handler.setPlayerRegistry(playerRegistry);
            s.setHandler(handler);
            return s;
        });

        if (session == null) return; // rejected (too many sessions)

        session.onPacket(content.retain());
    }

    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
        logger.error("Exception in Bedrock UDP server handler", cause);
    }
}
