package com.overmind.java;

import com.overmind.api.VertexGraphManager;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.nio.charset.StandardCharsets;

public class MinecraftLoginHandler extends ChannelInboundHandlerAdapter {
    private static final Logger logger = LoggerFactory.getLogger(MinecraftLoginHandler.class);
    
    private VertexGraphManager vertexGraphManager;
    private PlayerRegistry playerRegistry;

    public MinecraftLoginHandler() {
        this.vertexGraphManager = null; // Will be set by server
    }

    public void setVertexGraphManager(VertexGraphManager vertexGraphManager) {
        this.vertexGraphManager = vertexGraphManager;
    }

    public void setPlayerRegistry(PlayerRegistry playerRegistry) {
        this.playerRegistry = playerRegistry;
    }
    
    @Override
    public void channelActive(ChannelHandlerContext ctx) throws Exception {
        logger.info("Minecraft client connected: {}", ctx.channel().remoteAddress());
        super.channelActive(ctx);
    }
    
    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
        if (msg instanceof ByteBuf) {
            ByteBuf buf = (ByteBuf) msg;
            try {
                int packetLength = readVarInt(buf);
                int packetId = readVarInt(buf);
                
                logger.debug("Received packet: length={}, id={}", packetLength, packetId);
                
                switch (packetId) {
                    case 0x00: // Handshake packet
                        handleHandshake(ctx, buf);
                        break;
                    default:
                        logger.warn("Unknown packet ID: {}", packetId);
                        break;
                }

                // The client pipelines Handshake + Login Start (or Handshake + Status Request)
                // into the same TCP segment. After handleHandshake routes to the next handler,
                // any remaining bytes in this buf are the immediately-following packet.
                // Forward them so the new handler (LoginHandler / StatusHandler) receives them.
                if (buf.isReadable()) {
                    ctx.fireChannelRead(buf.retainedSlice());
                    buf.skipBytes(buf.readableBytes());
                }
            } finally {
                buf.release();
            }
        }
    }
    
    private void handleHandshake(ChannelHandlerContext ctx, ByteBuf buf) throws Exception {
        int protocolVersion = readVarInt(buf);
        String serverAddress = readString(buf);
        int serverPort = buf.readUnsignedShort();
        int nextState = readVarInt(buf);
        
        logger.info("Handshake: version={}, address={}, port={}, nextState={}", 
                   protocolVersion, serverAddress, serverPort, nextState);
        
        if (nextState == 1) {
            // Status request
            ctx.pipeline().addLast("statusHandler", new StatusHandler());
            ctx.pipeline().remove(this);
        } else if (nextState == 2) {
            // Login request
            LoginHandler loginHandler = new LoginHandler(vertexGraphManager, playerRegistry);
            ctx.pipeline().addLast("loginHandler", loginHandler);
            ctx.pipeline().remove(this);
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
        logger.error("Exception in Minecraft handler", cause);
        ctx.close();
    }
}
