package com.overmind.java;

import com.overmind.api.OverworldGenerator;
import com.overmind.api.VertexGraphManager;
import com.overmind.api.WorldStorage;
import com.overmind.api.bridge.OvermindBridge;
import com.overmind.java.plugin.PluginManager;
import io.netty.bootstrap.Bootstrap;
import io.netty.bootstrap.ServerBootstrap;
import io.netty.channel.*;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.SocketChannel;
import io.netty.channel.socket.nio.NioDatagramChannel;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.nio.file.Paths;

public class OvermindServer {
    private static final Logger logger = LoggerFactory.getLogger(OvermindServer.class);
    private static final int TCP_PORT     = 25565;
    private static final int BEDROCK_PORT = 19132;
    private static final int VIEW_DISTANCE = 8;
    private static final boolean OVERMIND_MODE = true; // Bedrock-Primary

    private EventLoopGroup bossGroup;
    private EventLoopGroup workerGroup;
    private Channel tcpChannel;
    private Channel udpChannel;
    private VertexGraphManager vertexGraphManager;
    private final PlayerRegistry playerRegistry = new PlayerRegistry();
    private final PluginManager pluginManager = new PluginManager();

    public void start() throws Exception {
        logger.info("Starting Overmind Server (TCP:{}, UDP:{})", TCP_PORT, BEDROCK_PORT);

        pluginManager.loadPlugins(Paths.get("plugins"));
        pluginManager.enableAll();

        long worldSeed = Long.parseLong(System.getProperty("overmind.seed", "12345"));
        WorldStorage storage = null;
        try {
            storage = new WorldStorage("overworld", Paths.get("server", "java"));
        } catch (IOException e) {
            logger.warn("Could not initialise world storage — world will not be persisted: {}", e.getMessage());
        }
        OverworldGenerator generator = new OverworldGenerator(worldSeed);
        vertexGraphManager = new VertexGraphManager(VIEW_DISTANCE, OVERMIND_MODE, storage, generator);

        bossGroup  = new NioEventLoopGroup(1);
        workerGroup = new NioEventLoopGroup();

        try {
            // ── TCP: Java Edition + proxied Bedrock (port 25565) ────────────
            final VertexGraphManager vgm = vertexGraphManager;
            final PlayerRegistry registry = playerRegistry;
            ServerBootstrap tcpBootstrap = new ServerBootstrap();
            tcpBootstrap.group(bossGroup, workerGroup)
                    .channel(NioServerSocketChannel.class)
                    .childHandler(new ChannelInitializer<SocketChannel>() {
                        @Override
                        protected void initChannel(SocketChannel ch) throws Exception {
                            ChannelPipeline pipeline = ch.pipeline();
                            ProtocolDetector detector = new ProtocolDetector();
                            detector.setVertexGraphManager(vgm);
                            detector.setPlayerRegistry(registry);
                            pipeline.addLast("protocolDetector", detector);
                        }
                    })
                    .option(ChannelOption.SO_BACKLOG, 128)
                    .childOption(ChannelOption.SO_KEEPALIVE, true)
                    .childOption(ChannelOption.TCP_NODELAY, true);

            ChannelFuture tcpFuture = tcpBootstrap.bind(TCP_PORT).sync();
            tcpChannel = tcpFuture.channel();
            logger.info("TCP listener started on port {} (Java + HTTP + proxied Bedrock)", TCP_PORT);

            if (OVERMIND_MODE) {
                // ── OVERMIND (Bedrock-Primary): Dragonfly Go engine owns :19132. ──
                // Start the bridge client that subscribes to Dragonfly's TCP event
                // stream (:25566) and registers Bedrock players into the shared
                // PlayerRegistry so Java clients can see them.
                DragonflybridgeClient bridgeClient =
                        new DragonflybridgeClient(playerRegistry);
                bridgeClient.start();
                OvermindBridge.INSTANCE.install(new OvermindPlayerStore(playerRegistry, bridgeClient));
                logger.info("OVERMIND mode: Dragonfly bridge client started (UDP :19132 owned by Go engine)");
            } else {
                // ── OVERJAVA (Java-Primary): Java server owns :19132 directly. ──
                Bootstrap udpBootstrap = new Bootstrap();
                udpBootstrap.group(workerGroup)
                        .channel(NioDatagramChannel.class)
                        .handler(new BedrockUdpServerHandler(vertexGraphManager, playerRegistry));

                ChannelFuture udpFuture = udpBootstrap.bind(BEDROCK_PORT).sync();
                udpChannel = udpFuture.channel();
                logger.info("UDP listener started on port {} (native Bedrock Edition)", BEDROCK_PORT);
            }

            logger.info("Overmind Server ready — seed={}", worldSeed);
            tcpChannel.closeFuture().sync();
        } finally {
            shutdown();
        }
    }

    public void shutdown() {
        logger.info("Shutting down Overmind Server");

        pluginManager.disableAll();
        if (vertexGraphManager != null) vertexGraphManager.shutdown();
        if (tcpChannel  != null) tcpChannel.close();
        if (udpChannel  != null) udpChannel.close();
        if (workerGroup != null) workerGroup.shutdownGracefully();
        if (bossGroup   != null) bossGroup.shutdownGracefully();

        logger.info("Overmind Server shutdown complete");
    }

    public VertexGraphManager getVertexGraphManager() {
        return vertexGraphManager;
    }

    public static void main(String[] args) {
        OvermindServer server = new OvermindServer();
        Runtime.getRuntime().addShutdownHook(new Thread(server::shutdown));
        try {
            server.start();
        } catch (Exception e) {
            logger.error("Failed to start Overmind Server", e);
            System.exit(1);
        }
    }
}
