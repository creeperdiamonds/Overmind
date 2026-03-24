package com.overmind.java;

import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import io.netty.handler.codec.http.*;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class HttpServerHandler extends ChannelInboundHandlerAdapter {
    private static final Logger logger = LoggerFactory.getLogger(HttpServerHandler.class);
    
    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
        if (msg instanceof HttpRequest) {
            HttpRequest request = (HttpRequest) msg;
            logger.info("HTTP request: {} {}", request.method(), request.uri());
            
            if (request.uri().equals("/")) {
                sendHomePage(ctx);
            } else if (request.uri().equals("/status")) {
                sendStatusPage(ctx);
            } else if (request.uri().startsWith("/resource-pack")) {
                sendResourcePack(ctx);
            } else if (request.uri().startsWith("/pack.mcpack")) {
                sendMcpack(ctx);
            } else {
                sendNotFound(ctx);
            }
        }
        
        if (msg instanceof HttpContent) {
            HttpContent content = (HttpContent) msg;
            content.release();
        }
    }
    
    private void sendHomePage(ChannelHandlerContext ctx) throws Exception {
        String html = "<!DOCTYPE html><html><head><title>Overmind Server</title></head>" +
                     "<body><h1>Overmind Server</h1><p>Bedrock-Primary Minecraft Server with Java Parity</p>" +
                     "<p><a href='/status'>Server Status</a></p>" +
                     "<p><a href='/resource-pack'>Download Java Resource Pack (.zip)</a></p>" +
                     "<p><a href='/pack.mcpack'>Download Bedrock Add-on Pack (.mcpack)</a></p></body></html>";
        
        FullHttpResponse response = new DefaultFullHttpResponse(
            HttpVersion.HTTP_1_1, 
            HttpResponseStatus.OK,
            ctx.alloc().buffer().writeBytes(html.getBytes())
        );
        
        response.headers().set(HttpHeaderNames.CONTENT_TYPE, "text/html");
        response.headers().set(HttpHeaderNames.CONTENT_LENGTH, response.content().readableBytes());
        response.headers().set(HttpHeaderNames.CONNECTION, HttpHeaderValues.CLOSE);
        
        ctx.writeAndFlush(response);
    }
    
    private void sendStatusPage(ChannelHandlerContext ctx) throws Exception {
        String json = "{\"server\":\"Overmind\",\"version\":\"1.21\",\"players\":0,\"maxPlayers\":100,\"motd\":\"Bedrock-Primary with Java Parity\"}";
        
        FullHttpResponse response = new DefaultFullHttpResponse(
            HttpVersion.HTTP_1_1,
            HttpResponseStatus.OK,
            ctx.alloc().buffer().writeBytes(json.getBytes())
        );
        
        response.headers().set(HttpHeaderNames.CONTENT_TYPE, "application/json");
        response.headers().set(HttpHeaderNames.CONTENT_LENGTH, response.content().readableBytes());
        response.headers().set(HttpHeaderNames.CONNECTION, HttpHeaderValues.CLOSE);
        
        ctx.writeAndFlush(response);
    }
    
    private void sendResourcePack(ChannelHandlerContext ctx) throws Exception {
        byte[] zipContent = new byte[]{0x50, 0x4B, 0x03, 0x04}; // Minimal ZIP header for placeholder
        
        FullHttpResponse response = new DefaultFullHttpResponse(
            HttpVersion.HTTP_1_1,
            HttpResponseStatus.OK,
            ctx.alloc().buffer().writeBytes(zipContent)
        );
        
        response.headers().set(HttpHeaderNames.CONTENT_TYPE, "application/zip");
        response.headers().set(HttpHeaderNames.CONTENT_LENGTH, response.content().readableBytes());
        response.headers().set(HttpHeaderNames.CONNECTION, HttpHeaderValues.CLOSE);
        response.headers().set(HttpHeaderNames.CONTENT_DISPOSITION, "attachment; filename=\"overmind-resource-pack.zip\"");
        
        ctx.writeAndFlush(response);
    }
    
    private void sendMcpack(ChannelHandlerContext ctx) throws Exception {
        // .mcpack is a ZIP file — stub with a minimal ZIP header until real pack content is generated
        byte[] zipContent = new byte[]{0x50, 0x4B, 0x03, 0x04}; // PK\x03\x04

        FullHttpResponse response = new DefaultFullHttpResponse(
            HttpVersion.HTTP_1_1,
            HttpResponseStatus.OK,
            ctx.alloc().buffer().writeBytes(zipContent)
        );

        response.headers().set(HttpHeaderNames.CONTENT_TYPE, "application/octet-stream");
        response.headers().set(HttpHeaderNames.CONTENT_LENGTH, response.content().readableBytes());
        response.headers().set(HttpHeaderNames.CONNECTION, HttpHeaderValues.CLOSE);
        response.headers().set(HttpHeaderNames.CONTENT_DISPOSITION, "attachment; filename=\"overmind.mcpack\"");

        ctx.writeAndFlush(response);
    }

    private void sendNotFound(ChannelHandlerContext ctx) throws Exception {
        FullHttpResponse response = new DefaultFullHttpResponse(
            HttpVersion.HTTP_1_1,
            HttpResponseStatus.NOT_FOUND,
            ctx.alloc().buffer().writeBytes("404 Not Found".getBytes())
        );
        
        response.headers().set(HttpHeaderNames.CONTENT_TYPE, "text/plain");
        response.headers().set(HttpHeaderNames.CONTENT_LENGTH, response.content().readableBytes());
        response.headers().set(HttpHeaderNames.CONNECTION, HttpHeaderValues.CLOSE);
        
        ctx.writeAndFlush(response);
    }
    
    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) throws Exception {
        logger.error("Exception in HTTP handler", cause);
        ctx.close();
    }
}
