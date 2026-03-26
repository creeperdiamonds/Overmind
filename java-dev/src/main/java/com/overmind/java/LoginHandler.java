package com.overmind.java;

import com.overmind.api.VertexGraphManager;
import io.netty.buffer.ByteBuf;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelInboundHandlerAdapter;
import io.netty.handler.codec.ByteToMessageDecoder;
import io.netty.handler.codec.MessageToByteEncoder;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import javax.crypto.Cipher;
import javax.crypto.spec.IvParameterSpec;
import javax.crypto.spec.SecretKeySpec;
import java.math.BigInteger;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.security.*;
import java.util.Arrays;
import java.util.List;
import java.util.UUID;

/**
 * Handles the Minecraft Java Edition Login state.
 *
 * <p>When {@link #onlineMode} is {@code true} (the default) the handler performs
 * full Mojang session authentication before admitting the player:
 * <ol>
 *   <li>C→S Login Start (0x00) — client sends username + UUID
 *   <li>S→C Encryption Request (0x01) — server sends RSA-1024 public key + 4-byte verify token
 *   <li>C→S Encryption Response (0x01) — client returns RSA-encrypted shared secret + verify token
 *   <li>Server verifies token, derives AES/CFB8 cipher, calls Mojang
 *       {@code hasJoined} API to confirm auth
 *   <li>S→C Login Success (0x02) — sent encrypted, carries the real Mojang UUID
 *   <li>C→S Login Acknowledged (0x03) — client transitions to Configuration state
 * </ol>
 *
 * <p>When {@code onlineMode} is {@code false} the Encryption Request step is
 * skipped and an offline-mode UUID is assigned.
 */
public class LoginHandler extends ChannelInboundHandlerAdapter {
    private static final Logger logger = LoggerFactory.getLogger(LoginHandler.class);

    // ── Online mode ──────────────────────────────────────────────────────────────
    /**
     * Online mode is ALWAYS enabled to comply with the Minecraft EULA.
     *
     * <p>The only way to disable it is to start the JVM with:
     * <pre>  -Dovermind.dev.offline=true</pre>
     * This is intentionally not a config-file option — disabling authentication
     * likely violates the Minecraft End User Licence Agreement and must never be
     * done accidentally or on a public-facing server.
     */
    static final boolean ONLINE_MODE_ENABLED = !Boolean.getBoolean("overmind.dev.offline");

    // ── RSA key pair (shared across all connections; generated once at class load) ─
    private static final KeyPair KEY_PAIR;
    static {
        try {
            KeyPairGenerator gen = KeyPairGenerator.getInstance("RSA");
            gen.initialize(1024);
            KEY_PAIR = gen.generateKeyPair();
            logger.info("RSA-1024 key pair generated for online-mode encryption");
            if (!ONLINE_MODE_ENABLED) {
                logger.warn("╔══════════════════════════════════════════════════════╗");
                logger.warn("║  OFFLINE MODE — EULA WARNING                         ║");
                logger.warn("║  Authentication is DISABLED (-Dovermind.dev.offline) ║");
                logger.warn("║  This likely violates the Minecraft EULA.            ║");
                logger.warn("║  NEVER run in this mode on a public-facing server.   ║");
                logger.warn("╚══════════════════════════════════════════════════════╝");
            }
        } catch (GeneralSecurityException e) {
            throw new ExceptionInInitializerError(e);
        }
    }

    // ── Per-connection state ──────────────────────────────────────────────────
    private enum LoginState { AWAITING_LOGIN_START, AWAITING_ENCRYPTION_RESPONSE, COMPLETE }

    private LoginState loginState = LoginState.AWAITING_LOGIN_START;
    private String     username   = "unknown";
    private byte[]     verifyToken;

    private final VertexGraphManager vertexGraphManager;
    private final PlayerRegistry     playerRegistry;

    public LoginHandler(VertexGraphManager vertexGraphManager,
                        PlayerRegistry playerRegistry) {
        this.vertexGraphManager = vertexGraphManager;
        this.playerRegistry     = playerRegistry;
    }

    // -------------------------------------------------------------------------
    // Inbound
    // -------------------------------------------------------------------------

    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
        if (!(msg instanceof ByteBuf)) return;
        ByteBuf buf = (ByteBuf) msg;
        try {
            readVarInt(buf); // consume length prefix
            int packetId = readVarInt(buf);

            switch (packetId) {
                case 0x00: // Login Start
                    if (loginState != LoginState.AWAITING_LOGIN_START) break;
                    username = readString(buf);
                    // 1.20.2+ Login Start carries the player UUID (16 bytes) — skip it.
                    if (buf.readableBytes() >= 16) buf.skipBytes(16);
                    logger.info("Login request from: {}", username);

                    if (ONLINE_MODE_ENABLED) {
                        sendEncryptionRequest(ctx);
                        loginState = LoginState.AWAITING_ENCRYPTION_RESPONSE;
                    } else {
                        UUID offlineUuid = offlineUuid(username);
                        sendLoginSuccess(ctx, username, offlineUuid);
                        loginState = LoginState.COMPLETE;
                    }
                    break;

                case 0x01: // Encryption Response
                    if (loginState != LoginState.AWAITING_ENCRYPTION_RESPONSE) break;
                    handleEncryptionResponse(ctx, buf);
                    break;

                case 0x03: // Login Acknowledged (1.20.2+)
                    if (loginState != LoginState.COMPLETE) break;
                    logger.info("Login acknowledged by {}, entering Configuration state", username);
                    ctx.pipeline().addLast("configHandler",
                            new ConfigurationHandler(username, vertexGraphManager, playerRegistry));
                    ctx.pipeline().remove(this);
                    break;

                default:
                    logger.warn("Unknown login packet 0x{}", Integer.toHexString(packetId));
                    break;
            }
        } finally {
            buf.release();
        }
    }

    // -------------------------------------------------------------------------
    // Online-mode: Encryption Request / Response
    // -------------------------------------------------------------------------

    /**
     * Sends Encryption Request (S→C 0x01).
     *
     * <pre>
     * String  server_id         (empty)
     * VarInt  public_key_length
     * Byte[]  public_key        (DER/X.509 SubjectPublicKeyInfo)
     * VarInt  verify_token_length
     * Byte[]  verify_token      (4 random bytes)
     * Boolean should_authenticate  true = require Mojang auth (protocol 766+)
     * </pre>
     */
    private void sendEncryptionRequest(ChannelHandlerContext ctx) throws Exception {
        verifyToken = new byte[4];
        new SecureRandom().nextBytes(verifyToken);

        byte[] pubKeyBytes = KEY_PAIR.getPublic().getEncoded();

        ByteBuf payload = ctx.alloc().buffer();
        writeVarInt(payload, PacketConstants.LOGIN_ENCRYPTION_REQUEST);
        writeString(payload, "");                          // server id (empty for modern clients)
        writeVarInt(payload, pubKeyBytes.length);
        payload.writeBytes(pubKeyBytes);
        writeVarInt(payload, verifyToken.length);
        payload.writeBytes(verifyToken);
        payload.writeBoolean(true);                        // should_authenticate = online mode

        ctx.writeAndFlush(frame(ctx, payload));
        logger.debug("Encryption Request sent to {}", username);
    }

    /**
     * Processes Encryption Response (C→S 0x01).
     *
     * <p>All RSA decryption and the blocking Mojang HTTP call happen on a
     * dedicated daemon thread.  {@code autoRead} is disabled while the thread
     * runs so no further client bytes are delivered to the handler until
     * after the cipher pipeline is installed.
     */
    private void handleEncryptionResponse(ChannelHandlerContext ctx, ByteBuf buf) {
        // Read both fields synchronously before spawning the auth thread.
        int ssLen = readVarInt(buf);
        byte[] encryptedSecret = new byte[ssLen];
        buf.readBytes(encryptedSecret);

        int vtLen = readVarInt(buf);
        byte[] encryptedVerifyToken = new byte[vtLen];
        buf.readBytes(encryptedVerifyToken);

        // Pause reading so Login Acknowledged doesn't arrive before the cipher is up.
        ctx.channel().config().setAutoRead(false);

        String capturedUsername = username;
        byte[] capturedVerifyToken = verifyToken;

        Thread authThread = new Thread(() -> {
            try {
                // Decrypt shared secret and verify token with server's RSA private key.
                Cipher rsa = Cipher.getInstance("RSA/ECB/PKCS1Padding");
                rsa.init(Cipher.DECRYPT_MODE, KEY_PAIR.getPrivate());

                byte[] sharedSecret      = rsa.doFinal(encryptedSecret);
                byte[] decryptedVerify   = rsa.doFinal(encryptedVerifyToken);

                if (!Arrays.equals(decryptedVerify, capturedVerifyToken)) {
                    logger.warn("Verify token mismatch for {} — possible MITM", capturedUsername);
                    ctx.channel().eventLoop().execute(() -> {
                        sendLoginDisconnect(ctx, "Encryption error — verify token mismatch.");
                    });
                    return;
                }

                // Compute the Minecraft-style signed-hex SHA-1 hash.
                String serverId = computeServerId(sharedSecret, KEY_PAIR.getPublic().getEncoded());

                // Verify with Mojang session server.
                UUID mojangUuid = verifyWithMojang(capturedUsername, serverId);
                if (mojangUuid == null) {
                    logger.warn("Mojang auth rejected for {}", capturedUsername);
                    ctx.channel().eventLoop().execute(() ->
                        sendLoginDisconnect(ctx,
                            "Failed to verify your username! " +
                            "Please make sure you own a valid copy of Minecraft."));
                    return;
                }

                // Build AES/CFB8 ciphers (shared secret = key AND IV).
                Cipher encryptCipher = aesCfb8(Cipher.ENCRYPT_MODE, sharedSecret);
                Cipher decryptCipher = aesCfb8(Cipher.DECRYPT_MODE, sharedSecret);

                ctx.channel().eventLoop().execute(() -> {
                    // Install cipher handlers at the head of the pipeline so
                    // all subsequent bytes on the wire are encrypted.
                    ctx.pipeline().addFirst("encrypt", new EncryptionEncoder(encryptCipher));
                    ctx.pipeline().addFirst("decrypt", new EncryptionDecoder(decryptCipher));
                    loginState = LoginState.COMPLETE;
                    ctx.channel().config().setAutoRead(true);
                    try {
                        sendLoginSuccess(ctx, capturedUsername, mojangUuid);
                    } catch (Exception e) {
                        logger.error("Failed to send Login Success to {}", capturedUsername, e);
                        ctx.close();
                    }
                });
                logger.info("Mojang auth succeeded for {} (uuid={})", capturedUsername, mojangUuid);

            } catch (Exception e) {
                logger.error("Auth error for {}", capturedUsername, e);
                ctx.channel().eventLoop().execute(() ->
                    sendLoginDisconnect(ctx, "Internal server error during authentication."));
            }
        }, "auth-" + capturedUsername);
        authThread.setDaemon(true);
        authThread.start();
    }

    // -------------------------------------------------------------------------
    // Outbound packets
    // -------------------------------------------------------------------------

    /**
     * Login Success (S→C 0x02) — protocol 766+ (1.20.6+) format:
     * <pre>
     * UUID    uuid
     * String  username
     * VarInt  properties_count  (0 for offline / online mode without skin relay)
     * </pre>
     */
    private void sendLoginSuccess(ChannelHandlerContext ctx, String name, UUID uuid) throws Exception {
        ByteBuf payload = ctx.alloc().buffer();
        writeVarInt(payload, 0x02);                     // packet id: Login Success
        payload.writeLong(uuid.getMostSignificantBits());
        payload.writeLong(uuid.getLeastSignificantBits());
        writeString(payload, name);
        writeVarInt(payload, 0);                        // 0 skin properties
        ctx.writeAndFlush(frame(ctx, payload));
        logger.info("Login Success sent to {} (uuid={})", name, uuid);
    }

    /**
     * Login Disconnect (S→C 0x00) — sends a JSON reason and closes the channel.
     */
    private void sendLoginDisconnect(ChannelHandlerContext ctx, String reason) {
        try {
            String json = "{\"text\":\"" + escapeJson(reason) + "\"}";
            ByteBuf payload = ctx.alloc().buffer();
            writeVarInt(payload, PacketConstants.LOGIN_DISCONNECT);
            writeString(payload, json);
            ctx.writeAndFlush(frame(ctx, payload))
               .addListener(ChannelFutureListener.CLOSE);
        } catch (Exception e) {
            ctx.close();
        }
    }

    // -------------------------------------------------------------------------
    // Cryptography helpers
    // -------------------------------------------------------------------------

    /**
     * Computes the Minecraft server-id hash used for Mojang session verification.
     *
     * <p>The hash is SHA-1 of {@code "" + sharedSecret + publicKey}, interpreted
     * as a signed big-endian integer and formatted as lowercase hex.  This matches
     * the twos-complement hex that Notchian clients send to {@code sessionserver.mojang.com}.
     */
    private static String computeServerId(byte[] sharedSecret, byte[] publicKey)
            throws NoSuchAlgorithmException {
        MessageDigest sha1 = MessageDigest.getInstance("SHA-1");
        sha1.update("".getBytes(StandardCharsets.UTF_8));
        sha1.update(sharedSecret);
        sha1.update(publicKey);
        // BigInteger handles the signed two's-complement representation automatically.
        return new BigInteger(sha1.digest()).toString(16);
    }

    /**
     * Calls the Mojang hasJoined endpoint and returns the player's UUID on success,
     * or {@code null} if authentication failed or the request timed out.
     */
    private static UUID verifyWithMojang(String name, String serverId) {
        try {
            URL url = java.net.URI.create(
                    "https://sessionserver.mojang.com/session/minecraft/hasJoined"
                    + "?username=" + java.net.URLEncoder.encode(name, "UTF-8")
                    + "&serverId="  + serverId).toURL();
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setConnectTimeout(5_000);
            conn.setReadTimeout(5_000);
            conn.setRequestMethod("GET");
            int status = conn.getResponseCode();
            if (status != 200) return null;          // 204 = not authenticated

            byte[] body = conn.getInputStream().readAllBytes();
            return parseUuidFromProfile(new String(body, StandardCharsets.UTF_8));
        } catch (Exception e) {
            logger.warn("Mojang session server unreachable: {}", e.getMessage());
            return null;
        }
    }

    /**
     * Minimal JSON scanner: extracts the {@code "id"} field from the Mojang profile
     * object.  Mojang returns UUIDs without dashes (32 hex chars); this method
     * inserts the canonical dashes before parsing.
     */
    private static UUID parseUuidFromProfile(String json) {
        int idx = json.indexOf("\"id\"");
        if (idx < 0) return null;
        int open  = json.indexOf('"', idx + 4);
        int close = json.indexOf('"', open + 1);
        if (open < 0 || close < 0) return null;
        String raw = json.substring(open + 1, close);
        if (raw.length() == 32) {
            raw = raw.substring(0, 8)  + "-" + raw.substring(8, 12)  + "-"
                + raw.substring(12, 16) + "-" + raw.substring(16, 20) + "-"
                + raw.substring(20);
        }
        try { return UUID.fromString(raw); }
        catch (IllegalArgumentException e) { return null; }
    }

    /** Creates an AES/CFB8/NoPadding cipher using {@code secret} as both key and IV. */
    private static Cipher aesCfb8(int mode, byte[] secret) throws GeneralSecurityException {
        SecretKeySpec key = new SecretKeySpec(secret, "AES");
        IvParameterSpec iv  = new IvParameterSpec(secret);
        Cipher cipher = Cipher.getInstance("AES/CFB8/NoPadding");
        cipher.init(mode, key, iv);
        return cipher;
    }

    // -------------------------------------------------------------------------
    // Cipher pipeline handlers (inner classes)
    // -------------------------------------------------------------------------

    /** Decrypts every incoming byte before the Login/Config/Play handlers see it. */
    private static final class EncryptionDecoder extends ByteToMessageDecoder {
        private final Cipher cipher;
        EncryptionDecoder(Cipher cipher) { this.cipher = cipher; }

        @Override
        protected void decode(ChannelHandlerContext ctx, ByteBuf in, List<Object> out) {
            byte[] bytes = new byte[in.readableBytes()];
            in.readBytes(bytes);
            byte[] plain = cipher.update(bytes);
            if (plain != null && plain.length > 0) {
                out.add(ctx.alloc().buffer(plain.length).writeBytes(plain));
            }
        }
    }

    /** Encrypts every outgoing byte before it hits the wire. */
    private static final class EncryptionEncoder extends MessageToByteEncoder<ByteBuf> {
        private final Cipher cipher;
        EncryptionEncoder(Cipher cipher) { this.cipher = cipher; }

        @Override
        protected void encode(ChannelHandlerContext ctx, ByteBuf msg, ByteBuf out) {
            byte[] bytes = new byte[msg.readableBytes()];
            msg.readBytes(bytes);
            byte[] encrypted = cipher.update(bytes);
            if (encrypted != null) out.writeBytes(encrypted);
        }
    }

    // -------------------------------------------------------------------------
    // Packet / encoding helpers
    // -------------------------------------------------------------------------

    /** Wraps a payload with a VarInt length prefix. Releases the payload buffer. */
    private ByteBuf frame(ChannelHandlerContext ctx, ByteBuf payload) {
        ByteBuf packet = ctx.alloc().buffer();
        writeVarInt(packet, payload.readableBytes());
        packet.writeBytes(payload);
        payload.release();
        return packet;
    }

    private void writeString(ByteBuf out, String str) {
        byte[] bytes = str.getBytes(StandardCharsets.UTF_8);
        writeVarInt(out, bytes.length);
        out.writeBytes(bytes);
    }

    private void writeVarInt(ByteBuf out, int value) {
        while (true) {
            if ((value & 0xFFFFFF80) == 0) { out.writeByte(value); return; }
            out.writeByte((value & 0x7F) | 0x80);
            value >>>= 7;
        }
    }

    private int readVarInt(ByteBuf in) {
        int value = 0, position = 0;
        byte b;
        do {
            if (position >= PacketConstants.VARINT_MAX_POSITION)
                throw new RuntimeException("VarInt too big");
            b = in.readByte();
            value |= (b & 0x7F) << position;
            if ((b & 0x80) == 0) break;
            position += 7;
        } while (true);
        return value;
    }

    private String readString(ByteBuf buf) {
        int length = readVarInt(buf);
        byte[] bytes = new byte[length];
        buf.readBytes(bytes);
        return new String(bytes, StandardCharsets.UTF_8);
    }

    private static String escapeJson(String s) {
        return s.replace("\\", "\\\\").replace("\"", "\\\"");
    }

    /** Generates an offline-mode UUID from the player name (matches vanilla behaviour). */
    private static UUID offlineUuid(String name) {
        return UUID.nameUUIDFromBytes(("OfflinePlayer:" + name).getBytes(StandardCharsets.UTF_8));
    }

    @Override
    public void exceptionCaught(ChannelHandlerContext ctx, Throwable cause) {
        logger.error("Exception in Login handler for {}", username, cause);
        ctx.close();
    }
}
