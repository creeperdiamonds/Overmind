package com.overmind.java;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.channel.*;
import io.netty.channel.socket.DatagramPacket;
import io.netty.util.Attribute;
import io.netty.util.AttributeKey;
import io.netty.util.concurrent.EventExecutor;

import java.net.InetSocketAddress;
import java.net.SocketAddress;

/**
 * Minimal {@link ChannelHandlerContext} adapter that routes {@code writeAndFlush} calls
 * from {@link BedrockLoginHandler} back to the shared UDP channel, addressed to the
 * specific client's {@link InetSocketAddress}.
 *
 * <p>Only the methods called by {@code BedrockLoginHandler} are implemented.
 * All other lifecycle methods throw {@link UnsupportedOperationException} to
 * surface any unexpected usage.
 */
public class UdpChannelContextAdapter implements ChannelHandlerContext {

    private final ChannelHandlerContext realCtx;
    private final InetSocketAddress     remoteAddress;

    public UdpChannelContextAdapter(ChannelHandlerContext realCtx,
                                    InetSocketAddress remoteAddress) {
        this.realCtx       = realCtx;
        this.remoteAddress = remoteAddress;
    }

    @Override
    public ChannelFuture writeAndFlush(Object msg) {
        if (msg instanceof ByteBuf) {
            ByteBuf buf = (ByteBuf) msg;
            return realCtx.writeAndFlush(new DatagramPacket(buf, remoteAddress));
        }
        return realCtx.writeAndFlush(msg);
    }

    @Override
    public ChannelFuture writeAndFlush(Object msg, ChannelPromise promise) {
        if (msg instanceof ByteBuf) {
            ByteBuf buf = (ByteBuf) msg;
            return realCtx.writeAndFlush(new DatagramPacket(buf, remoteAddress), promise);
        }
        return realCtx.writeAndFlush(msg, promise);
    }

    @Override
    public ByteBufAllocator alloc() {
        return realCtx.alloc();
    }

    @Override
    public Channel channel() {
        return realCtx.channel();
    }

    @Override
    public EventExecutor executor() {
        return realCtx.executor();
    }

    @Override
    public ChannelPromise newPromise() {
        return realCtx.newPromise();
    }

    @Override
    public ChannelProgressivePromise newProgressivePromise() {
        return realCtx.newProgressivePromise();
    }

    @Override
    public ChannelFuture newSucceededFuture() {
        return realCtx.newSucceededFuture();
    }

    @Override
    public ChannelFuture newFailedFuture(Throwable cause) {
        return realCtx.newFailedFuture(cause);
    }

    @Override
    public ChannelPromise voidPromise() {
        return realCtx.voidPromise();
    }

    @Override
    public ChannelPipeline pipeline() {
        return realCtx.pipeline();
    }

    @Override
    public ChannelFuture close() {
        return realCtx.newSucceededFuture(); // no-op: UDP sessions don't close the shared channel
    }

    @Override
    public ChannelFuture close(ChannelPromise promise) {
        promise.setSuccess();
        return promise;
    }

    @Override
    public ChannelFuture disconnect() { return realCtx.newSucceededFuture(); }

    @Override
    public ChannelFuture disconnect(ChannelPromise promise) { promise.setSuccess(); return promise; }

    @Override
    public ChannelFuture deregister() { return realCtx.newSucceededFuture(); }

    @Override
    public ChannelFuture deregister(ChannelPromise promise) { promise.setSuccess(); return promise; }

    // ── Unsupported (not called by BedrockLoginHandler) ───────────────────────

    @Override public String name() { return "udpAdapter"; }
    @Override public ChannelHandler handler() { throw new UnsupportedOperationException(); }
    @Override public boolean isRemoved() { return false; }
    @Override public ChannelHandlerContext fireChannelRegistered() { return this; }
    @Override public ChannelHandlerContext fireChannelUnregistered() { return this; }
    @Override public ChannelHandlerContext fireChannelActive() { return this; }
    @Override public ChannelHandlerContext fireChannelInactive() { return this; }
    @Override public ChannelHandlerContext fireExceptionCaught(Throwable cause) { return this; }
    @Override public ChannelHandlerContext fireUserEventTriggered(Object evt) { return this; }
    @Override public ChannelHandlerContext fireChannelRead(Object msg) { return this; }
    @Override public ChannelHandlerContext fireChannelReadComplete() { return this; }
    @Override public ChannelHandlerContext fireChannelWritabilityChanged() { return this; }
    @Override public ChannelHandlerContext read() { return this; }
    @Override public ChannelHandlerContext flush() { realCtx.flush(); return this; }
    @Override public ChannelFuture bind(SocketAddress addr) { throw new UnsupportedOperationException(); }
    @Override public ChannelFuture bind(SocketAddress addr, ChannelPromise p) { throw new UnsupportedOperationException(); }
    @Override public ChannelFuture connect(SocketAddress addr) { throw new UnsupportedOperationException(); }
    @Override public ChannelFuture connect(SocketAddress addr, ChannelPromise p) { throw new UnsupportedOperationException(); }
    @Override public ChannelFuture connect(SocketAddress a, SocketAddress b) { throw new UnsupportedOperationException(); }
    @Override public ChannelFuture connect(SocketAddress a, SocketAddress b, ChannelPromise p) { throw new UnsupportedOperationException(); }
    @Override public ChannelFuture write(Object msg) { return writeAndFlush(msg); }
    @Override public ChannelFuture write(Object msg, ChannelPromise p) { return writeAndFlush(msg, p); }
    @Override public <T> Attribute<T> attr(AttributeKey<T> key) { return realCtx.channel().attr(key); }
    @Override public <T> boolean hasAttr(AttributeKey<T> key) { return realCtx.channel().hasAttr(key); }
}
