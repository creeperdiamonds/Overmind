package com.overmind.java;

import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.socket.DatagramPacket;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.net.InetSocketAddress;

/**
 * Represents one UDP-connected Bedrock client.
 *
 * <p>Because Netty's UDP channel is a single shared channel (not one per connection like TCP),
 * each session wraps a virtual {@link ChannelHandlerContext}-like adapter that routes outbound
 * writes back as {@link DatagramPacket}s addressed to this client's IP:port.
 *
 * <h3>Design</h3>
 * {@link BedrockLoginHandler} was written for TCP (assumes {@code ctx.writeAndFlush(ByteBuf)}).
 * The {@link UdpChannelContextAdapter} wraps the shared UDP channel context and transparently
 * re-addresses every write to this session's remote address, so {@code BedrockLoginHandler} needs
 * no changes.
 */
public class BedrockUdpSession {
    private static final Logger logger = LoggerFactory.getLogger(BedrockUdpSession.class);

    private final InetSocketAddress       remoteAddress;
    private final UdpChannelContextAdapter ctxAdapter;
    private BedrockLoginHandler           handler;

    public BedrockUdpSession(ChannelHandlerContext realCtx,
                              InetSocketAddress remoteAddress) {
        this.remoteAddress = remoteAddress;
        this.ctxAdapter    = new UdpChannelContextAdapter(realCtx, remoteAddress);
    }

    public void setHandler(BedrockLoginHandler handler) {
        this.handler = handler;
    }

    /**
     * Delivers an inbound datagram payload to the {@link BedrockLoginHandler}.
     * The buf's ref-count is transferred; this method releases it.
     */
    public void onPacket(ByteBuf buf) {
        try {
            handler.channelRead(ctxAdapter, buf);
        } catch (Exception e) {
            logger.error("BedrockUdpSession: error processing packet from {}: {}",
                    remoteAddress, e.getMessage(), e);
            buf.release();
        }
    }

    /** Returns the remote address of this Bedrock client. */
    public InetSocketAddress getRemoteAddress() {
        return remoteAddress;
    }
}
