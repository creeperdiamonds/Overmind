package com.overmind.java;

import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class StatusHandler extends ChannelInboundHandlerAdapter {
    private static final Logger logger = LoggerFactory.getLogger(StatusHandler.class);
    
    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
        if (msg instanceof ByteBuf) {
            ByteBuf buf = (ByteBuf) msg;
            try {
                readVarInt(buf); // consume length prefix
                int packetId = readVarInt(buf);
                
                if (packetId == 0x00) { // Status request
                    logger.info("Received status request");
                    sendStatusResponse(ctx);
                } else if (packetId == 0x01) { // Ping request
                    long payload = buf.readLong();
                    logger.info("Received ping request: {}", payload);
                    sendPingResponse(ctx, payload);
                }
            } finally {
                buf.release();
            }
        }
    }
    
    private void sendStatusResponse(ChannelHandlerContext ctx) throws Exception {
        String jsonResponse = "{\"version\":{\"name\":\"Overmind 1.21\",\"protocol\":763},\"players\":{\"max\":100,\"online\":0},\"description\":{\"text\":\"Overmind Server - Bedrock-Primary with Java Parity\"}}";
        
        ByteBuf response = ctx.alloc().buffer();
        writeVarInt(response, jsonResponse.length());
        response.writeBytes(jsonResponse.getBytes("UTF-8"));
        
        ByteBuf packet = ctx.alloc().buffer();
        writeVarInt(packet, response.readableBytes());
        packet.writeBytes(response);
        
        ctx.writeAndFlush(packet);
        response.release();
    }
    
    private void sendPingResponse(ChannelHandlerContext ctx, long payload) throws Exception {
        ByteBuf response = ctx.alloc().buffer();
        writeVarInt(response, 9); // Packet length (1 byte for ID + 8 bytes for payload)
        writeVarInt(response, 0x01); // Packet ID
        response.writeLong(payload);
        
        ctx.writeAndFlush(response);
    }
    
    private void writeVarInt(ByteBuf out, int value) {
        while (true) {
            if ((value & 0xFFFFFF80) == 0) {
                out.writeByte(value);
                break;
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
    
    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
        logger.error("Exception in Status handler", cause);
        ctx.close();
    }
}
