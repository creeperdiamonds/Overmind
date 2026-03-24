package com.overmind.java;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.PrintWriter;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Connects to the Dragonfly Go engine's bridge server (default: localhost:25566)
 * and fans every received Bedrock player event into the shared {@link PlayerRegistry}.
 *
 * <p>Events are newline-delimited JSON objects:
 * <pre>
 * {"type":"join",  "name":"Steve", "x":0.0, "y":100.0, "z":0.0}
 * {"type":"move",  "name":"Steve", "x":1.2, "y":100.0, "z":-3.4, "yaw":90.0, "pitch":0.0}
 * {"type":"leave", "name":"Steve"}
 * </pre>
 *
 * <p>On "join" a virtual {@link PlayerRegistry.PlayerEntry} is created for the Bedrock
 * player so that every Java-side {@link PlayHandler} receives the standard
 * {@code onPlayerJoined / onPlayerMoved / onPlayerLeft} callbacks and spawns the
 * corresponding player entity on each Java client.  Dragonfly handles the
 * Bedrock-side rendering natively — the {@link BedrockListener} is intentionally
 * a no-op because no packets need to be sent back to the Go engine.
 */
public class DragonflybridgeClient {
    private static final Logger logger = LoggerFactory.getLogger(DragonflybridgeClient.class);

    private static final String DEFAULT_HOST = "localhost";
    private static final int    DEFAULT_PORT = 25566;

    private final String         host;
    private final int            port;
    private final PlayerRegistry registry;

    /** Tracks registered Bedrock entries so we can unregister them cleanly. */
    private final ConcurrentHashMap<String, PlayerRegistry.PlayerEntry> bedrockPlayers =
            new ConcurrentHashMap<>();

    /** Output writer — replaced atomically on each reconnect; null when disconnected. */
    private final AtomicReference<PrintWriter> writer = new AtomicReference<>();

    public DragonflybridgeClient(PlayerRegistry registry) {
        this(DEFAULT_HOST, DEFAULT_PORT, registry);
    }

    public DragonflybridgeClient(String host, int port, PlayerRegistry registry) {
        this.host     = host;
        this.port     = port;
        this.registry = registry;
    }

    /**
     * Starts the bridge client on a daemon thread.  Reconnection is attempted
     * automatically every 5 seconds when the Dragonfly engine is not yet running.
     */
    public void start() {
        Thread t = new Thread(this::runLoop, "dragonfly-bridge-client");
        t.setDaemon(true);
        t.start();
        logger.info("Dragonfly bridge client starting — connecting to {}:{}", host, port);
    }

    // ─── Outbound commands (Java → Go) ────────────────────────────────────────

    /** Disconnects a Bedrock player with a reason visible on their screen. */
    public void kickPlayer(String name, String reason) {
        sendCommand("{\"cmd\":\"kick\",\"name\":\"" + esc(name) + "\",\"reason\":\"" + esc(reason) + "\"}");
    }

    /** Sends a chat message to a single Bedrock player. */
    public void messagePlayer(String name, String text) {
        sendCommand("{\"cmd\":\"message\",\"name\":\"" + esc(name) + "\",\"text\":\"" + esc(text) + "\"}");
    }

    /** Broadcasts a message to every online Bedrock player. */
    public void broadcast(String text) {
        sendCommand("{\"cmd\":\"broadcast\",\"text\":\"" + esc(text) + "\"}");
    }

    /** Teleports a Bedrock player to the given world coordinates. */
    public void teleportPlayer(String name, double x, double y, double z) {
        sendCommand(String.format("{\"cmd\":\"teleport\",\"name\":\"%s\",\"x\":%s,\"y\":%s,\"z\":%s}",
                esc(name), x, y, z));
    }

    private void sendCommand(String json) {
        PrintWriter pw = writer.get();
        if (pw == null) {
            logger.warn("Dragonfly bridge: cannot send command — not connected");
            return;
        }
        pw.println(json);
    }

    private static String esc(String s) {
        return s.replace("\\", "\\\\").replace("\"", "\\\"");
    }

    // ─── Internal ─────────────────────────────────────────────────────────────

    private void runLoop() {
        while (!Thread.currentThread().isInterrupted()) {
            try (Socket sock = new Socket(host, port);
                 BufferedReader reader = new BufferedReader(
                         new InputStreamReader(sock.getInputStream(), StandardCharsets.UTF_8));
                 PrintWriter pw = new PrintWriter(sock.getOutputStream(), true)) {

                writer.set(pw);
                logger.info("Dragonfly bridge connected to {}:{}", host, port);
                String line;
                while ((line = reader.readLine()) != null) {
                    processEvent(line.trim());
                }
                writer.set(null);
                logger.warn("Dragonfly bridge connection closed — reconnecting in 5 s");

            } catch (Exception e) {
                logger.warn("Dragonfly bridge unavailable ({}:{}) — retrying in 5 s: {}",
                        host, port, e.getMessage());
            }
            try { Thread.sleep(5_000); } catch (InterruptedException ie) { return; }
        }
    }

    private void processEvent(String json) {
        if (json.isEmpty()) return;
        try {
            JsonObject obj  = JsonParser.parseString(json).getAsJsonObject();
            String     type = obj.get("type").getAsString();
            String     name = obj.get("name").getAsString();

            switch (type) {
                case "join":
                    onJoin(name,
                            getDouble(obj, "x", 0),
                            getDouble(obj, "y", 100),
                            getDouble(obj, "z", 0));
                    break;
                case "move":
                    onMove(name,
                            getDouble(obj, "x", 0),
                            getDouble(obj, "y", 0),
                            getDouble(obj, "z", 0),
                            (float) getDouble(obj, "yaw",   0),
                            (float) getDouble(obj, "pitch", 0));
                    break;
                case "leave":
                    onLeave(name);
                    break;
                default:
                    logger.debug("Dragonfly bridge: unknown event type '{}'", type);
            }
        } catch (Exception e) {
            logger.warn("Dragonfly bridge: failed to parse event '{}': {}", json, e.getMessage());
        }
    }

    // ─── Event handlers ───────────────────────────────────────────────────────

    private void onJoin(String name, double x, double y, double z) {
        if (bedrockPlayers.containsKey(name)) return; // guard double-join
        PlayerRegistry.PlayerEntry entry = registry.register(name, true, x, y, z,
                new BedrockListener());
        bedrockPlayers.put(name, entry);
        logger.info("Bedrock player '{}' joined via Dragonfly bridge at ({}, {}, {})",
                name, x, y, z);
    }

    private void onMove(String name, double x, double y, double z, float yaw, float pitch) {
        PlayerRegistry.PlayerEntry entry = bedrockPlayers.get(name);
        if (entry == null) return;
        registry.updatePosition(entry, x, y, z, yaw, pitch, yaw);
    }

    private void onLeave(String name) {
        bedrockPlayers.remove(name);
        registry.unregister(name);
        logger.info("Bedrock player '{}' left via Dragonfly bridge", name);
    }

    // ─── Bedrock PlayerListener ───────────────────────────────────────────────

    /**
     * No-op listener for virtual Bedrock players.  Bedrock players receive their
     * own world updates directly from Dragonfly; only Java-side PlayHandler
     * listeners need to act on these callbacks to spawn/move/despawn the entity
     * on Java clients.
     */
    private static class BedrockListener implements PlayerRegistry.PlayerListener {
        @Override public void onPlayerJoined(PlayerRegistry.PlayerEntry joiner) {}
        @Override public void onPlayerLeft(PlayerRegistry.PlayerEntry leaver)   {}
        @Override public void onPlayerMoved(PlayerRegistry.PlayerEntry mover)   {}
    }

    // ─── Helpers ──────────────────────────────────────────────────────────────

    private static double getDouble(JsonObject obj, String key, double def) {
        return obj.has(key) ? obj.get(key).getAsDouble() : def;
    }
}
