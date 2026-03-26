package com.overmind.java;

import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFutureListener;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.Collection;
import java.util.LinkedHashSet;
import java.util.Set;

/**
 * Reads operator commands from {@code stdin} on a dedicated daemon thread and
 * dispatches them to the live server state.
 *
 * <p>Supported commands:
 * <ul>
 *   <li>{@code stop}                — graceful server shutdown</li>
 *   <li>{@code list}                — print online player names</li>
 *   <li>{@code kick <player> [reason]} — disconnect a player</li>
 *   <li>{@code say <message>}       — broadcast a system chat message</li>
 * </ul>
 */
public class ServerConsole {
    private static final Logger logger  = LoggerFactory.getLogger(ServerConsole.class);
    private static final Path   OPS_FILE = Paths.get("ops.txt");

    private final PlayerRegistry playerRegistry;
    private final Runnable       stopHook;

    public ServerConsole(PlayerRegistry playerRegistry, Runnable stopHook) {
        this.playerRegistry = playerRegistry;
        this.stopHook       = stopHook;
    }

    /** Starts the console read loop on a daemon thread and returns immediately. */
    public void start() {
        Thread t = new Thread(this::run, "console");
        t.setDaemon(true);
        t.start();
        logger.info("Console ready — type 'help' for commands");
    }

    // -------------------------------------------------------------------------
    // Read loop
    // -------------------------------------------------------------------------

    private void run() {
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(System.in))) {
            String line;
            while ((line = reader.readLine()) != null) {
                dispatch(line.trim());
            }
        } catch (Exception e) {
            logger.error("Console read error", e);
        }
    }

    // -------------------------------------------------------------------------
    // Command dispatch
    // -------------------------------------------------------------------------

    private void dispatch(String line) {
        if (line.isEmpty()) return;
        // Accept both "kick player" and "/kick player"
        if (line.startsWith("/")) line = line.substring(1);
        String[] parts = line.split("\\s+", 2);
        String   cmd   = parts[0].toLowerCase();
        String   args  = parts.length > 1 ? parts[1] : "";

        switch (cmd) {
            case "stop":
                System.out.println("[Console] Stopping server...");
                logger.info("Console: stop requested");
                stopHook.run();
                break;

            case "list":
                printPlayerList();
                break;

            case "kick":
                if (args.isEmpty()) {
                    System.out.println("Usage: kick <player> [reason]");
                    return;
                }
                String[] kp = args.split("\\s+", 2);
                kickPlayer(kp[0], kp.length > 1 ? kp[1] : "Kicked by an operator");
                break;

            case "say":
                if (args.isEmpty()) {
                    System.out.println("Usage: say <message>");
                    return;
                }
                broadcastMessage("[Server] " + args);
                break;

            case "op":
                if (args.isEmpty()) { System.out.println("Usage: op <player>"); return; }
                opPlayer(args.trim());
                break;

            case "deop":
                if (args.isEmpty()) { System.out.println("Usage: deop <player>"); return; }
                deopPlayer(args.trim());
                break;

            case "ops":
                printOps();
                break;

            case "help":
                System.out.println("Commands: stop | list | kick <player> [reason] | say <message>"
                    + " | op <player> | deop <player> | ops | help");
                break;

            default:
                System.out.println("Unknown command '" + cmd + "'. Type 'help' for a list.");
                break;
        }
    }

    // -------------------------------------------------------------------------
    // Command implementations
    // -------------------------------------------------------------------------

    private void printPlayerList() {
        Collection<PlayerRegistry.PlayerEntry> players = playerRegistry.getAll();
        if (players.isEmpty()) {
            System.out.println("There are no players online.");
        } else {
            StringBuilder sb = new StringBuilder("Online (").append(players.size()).append("): ");
            players.forEach(p -> sb.append(p.username).append(", "));
            System.out.println(sb.substring(0, sb.length() - 2));
        }
    }

    private void kickPlayer(String username, String reason) {
        PlayerRegistry.PlayerEntry entry = playerRegistry.getPlayer(username);
        if (entry == null || entry.channel == null || !entry.channel.isActive()) {
            System.out.println("Player '" + username + "' is not online.");
            return;
        }
        Channel ch = entry.channel;
        ch.eventLoop().execute(() -> {
            try {
                String json = "{\"text\":\"" + escapeJson(reason) + "\"}";
                ByteBuf payload = ch.alloc().buffer();
                writeVarInt(payload, PacketConstants.PLAY_DISCONNECT);
                writeString(payload, json);
                ByteBuf framed = ch.alloc().buffer();
                writeVarInt(framed, payload.readableBytes());
                framed.writeBytes(payload);
                payload.release();
                ch.writeAndFlush(framed).addListener(ChannelFutureListener.CLOSE);
            } catch (Exception e) {
                ch.close();
            }
        });
        logger.info("Console: kicked '{}' — {}", username, reason);
        System.out.println("Kicked " + username + ": " + reason);
    }

    /**
     * Sends a System Chat Message (S→C) to every online Java player.
     * Dispatches onto each player's own event loop to stay thread-safe.
     */
    private void broadcastMessage(String message) {
        String json = "{\"text\":\"" + escapeJson(message) + "\"}";
        for (PlayerRegistry.PlayerEntry entry : playerRegistry.getAll()) {
            Channel ch = entry.channel;
            if (ch == null || !ch.isActive()) continue;
            ch.eventLoop().execute(() -> {
                try {
                    ByteBuf payload = ch.alloc().buffer();
                    writeVarInt(payload, PacketConstants.PLAY_SYSTEM_CHAT);
                    writeString(payload, json);
                    payload.writeBoolean(false);        // overlay = false (normal chat area)
                    ByteBuf framed = ch.alloc().buffer();
                    writeVarInt(framed, payload.readableBytes());
                    framed.writeBytes(payload);
                    payload.release();
                    ch.writeAndFlush(framed);
                } catch (Exception e) {
                    logger.warn("Failed to send chat to {}", entry.username, e);
                }
            });
        }
        System.out.println(message);
        logger.info("Console broadcast: {}", message);
    }

    // -------------------------------------------------------------------------
    // Operator management
    // -------------------------------------------------------------------------

    private void opPlayer(String username) {
        Set<String> ops = loadOps();
        if (ops.add(username)) {
            saveOps(ops);
            System.out.println("Made " + username + " a server operator.");
            logger.info("Console: opped '{}'", username);
        } else {
            System.out.println(username + " is already an operator.");
        }
    }

    private void deopPlayer(String username) {
        Set<String> ops = loadOps();
        if (ops.remove(username)) {
            saveOps(ops);
            System.out.println("Removed " + username + " from operators.");
            logger.info("Console: de-opped '{}'", username);
        } else {
            System.out.println(username + " is not in the operators list.");
        }
    }

    private void printOps() {
        Set<String> ops = loadOps();
        if (ops.isEmpty()) {
            System.out.println("No operators configured.");
        } else {
            System.out.println("Operators (" + ops.size() + "): " + String.join(", ", ops));
        }
    }

    private static Set<String> loadOps() {
        Set<String> ops = new LinkedHashSet<>();
        if (!Files.exists(OPS_FILE)) return ops;
        try {
            Files.lines(OPS_FILE, StandardCharsets.UTF_8)
                 .map(String::trim)
                 .filter(l -> !l.isEmpty())
                 .forEach(ops::add);
        } catch (Exception e) {
            logger.warn("Could not read ops.txt", e);
        }
        return ops;
    }

    private static void saveOps(Set<String> ops) {
        try {
            String content = ops.isEmpty() ? "" : String.join("\n", ops) + "\n";
            Files.writeString(OPS_FILE, content, StandardCharsets.UTF_8);
        } catch (Exception e) {
            logger.warn("Could not write ops.txt", e);
        }
    }

    // -------------------------------------------------------------------------
    // Wire-encoding helpers
    // -------------------------------------------------------------------------

    private static void writeVarInt(ByteBuf out, int value) {
        while (true) {
            if ((value & 0xFFFFFF80) == 0) { out.writeByte(value); return; }
            out.writeByte((value & 0x7F) | 0x80);
            value >>>= 7;
        }
    }

    private static void writeString(ByteBuf out, String str) {
        byte[] bytes = str.getBytes(StandardCharsets.UTF_8);
        writeVarInt(out, bytes.length);
        out.writeBytes(bytes);
    }

    private static String escapeJson(String s) {
        return s.replace("\\", "\\\\").replace("\"", "\\\"");
    }
}
