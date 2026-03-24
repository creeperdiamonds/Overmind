package com.overmind.java;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import io.netty.channel.ChannelHandlerContext;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.TreeMap;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * Implements the RakNet reliability and ordering layer for a single Bedrock client connection.
 *
 * <h3>Responsibilities</h3>
 * <ul>
 *   <li>Assign outgoing sequence numbers (one per datagram)</li>
 *   <li>Send ACK datagrams when reliable inbound datagrams are received</li>
 *   <li>Send NAK datagrams when sequence gaps are detected</li>
 *   <li>Buffer outgoing reliable packets for retransmission on timeout (basic implementation)</li>
 *   <li>Track the highest received sequence number for gap detection</li>
 * </ul>
 *
 * <h3>RakNet datagram format</h3>
 * <pre>
 *   byte     flags      — 0x84 for DATA packets (bit 7 = VALID, bit 2 = ACK_RECEIPT_RECORD)
 *   int24 LE sequenceNum
 *   &lt;encapsulated packets…&gt;
 * </pre>
 *
 * <h3>ACK / NAK format</h3>
 * <pre>
 *   byte     type        — 0xC0 (ACK) or 0xA0 (NAK)
 *   short BE record_count
 *   For each record:
 *     byte  is_range     — 0 = single, 1 = range
 *     int24 LE start_seq
 *     if is_range: int24 LE end_seq
 * </pre>
 */
public class RakNetReliabilityLayer {
    private static final Logger logger = LoggerFactory.getLogger(RakNetReliabilityLayer.class);

    /** Maximum time (ms) to wait before retransmitting an unacknowledged packet. */
    private static final long RETRANSMIT_TIMEOUT_MS = 500;

    private final AtomicInteger outSeq        = new AtomicInteger(0);
    private final AtomicInteger outMessageSeq = new AtomicInteger(0);

    // Highest received datagram sequence number (for NAK gap detection)
    private int lastReceivedSeq = -1;

    // Pending reliable sends: outSeq → (timestamp, payload)
    private final TreeMap<Integer, PendingPacket> pending = new TreeMap<>();

    private static final class PendingPacket {
        final long    sentMs;
        final ByteBuf payload; // full framed datagram
        PendingPacket(long sentMs, ByteBuf payload) {
            this.sentMs  = sentMs;
            this.payload = payload;
        }
    }

    // ── Inbound ACK / NAK processing ─────────────────────────────────────────

    /**
     * Processes an inbound ACK or NAK datagram.
     *
     * @param buf    the ACK/NAK payload (reader index positioned after the type byte)
     * @param isAck  {@code true} for ACK (0xC0), {@code false} for NAK (0xA0)
     * @param ctx    channel context (used for retransmission on NAK)
     */
    public void processAckOrNak(ByteBuf buf, boolean isAck, ChannelHandlerContext ctx) {
        int recordCount = buf.readShort() & 0xFFFF;
        for (int i = 0; i < recordCount; i++) {
            boolean isRange = buf.readByte() == 0; // 0 = single, 1 = range (inverted in some impls)
            int start = readInt24LE(buf);
            int end   = isRange ? readInt24LE(buf) : start;

            if (isAck) {
                for (int seq = start; seq <= end; seq++) {
                    PendingPacket pp = pending.remove(seq);
                    if (pp != null) pp.payload.release();
                }
                logger.debug("RakNet ACK: seq {}-{}", start, end);
            } else {
                // NAK: retransmit missing datagrams
                for (int seq = start; seq <= end; seq++) {
                    PendingPacket pp = pending.get(seq);
                    if (pp != null) {
                        logger.debug("RakNet NAK retransmit: seq {}", seq);
                        ctx.writeAndFlush(pp.payload.retainedDuplicate());
                    }
                }
            }
        }
    }

    /**
     * Called when a DATA datagram (0x80–0x8F) is received.
     * Records the sequence number, detects gaps, and sends an ACK (and NAK for gaps).
     *
     * @param seqNum received datagram sequence number
     * @param ctx    channel context for sending ACK/NAK
     */
    public void onDatagramReceived(int seqNum, ChannelHandlerContext ctx) {
        if (lastReceivedSeq >= 0 && seqNum > lastReceivedSeq + 1) {
            // Gap detected — send NAK for the missing range
            sendNak(ctx, lastReceivedSeq + 1, seqNum - 1);
        }
        if (seqNum > lastReceivedSeq) lastReceivedSeq = seqNum;
        sendAck(ctx, seqNum, seqNum);
    }

    // ── Outbound ─────────────────────────────────────────────────────────────

    /**
     * Wraps an encapsulated packet payload into a RakNet DATA datagram (0x84 frame),
     * assigns a sequence number, and registers the datagram for potential retransmission.
     *
     * @param alloc      Netty allocator
     * @param payload    encapsulated packet payload (reliability header + data), caller must release
     * @param reliable   if {@code true}, the datagram is buffered for retransmission until ACKed
     * @return framed datagram ready to send via {@code ctx.writeAndFlush}
     */
    public ByteBuf wrapDatagramReliable(ByteBufAllocator alloc, ByteBuf payload, boolean reliable) {
        int seq = outSeq.getAndIncrement();
        ByteBuf datagram = alloc.buffer(4 + payload.readableBytes());
        datagram.writeByte(0x84);        // DATA flag
        writeInt24LE(datagram, seq);
        datagram.writeBytes(payload);

        if (reliable) {
            pending.put(seq, new PendingPacket(System.currentTimeMillis(), datagram.retainedDuplicate()));
        }
        return datagram;
    }

    /**
     * Wraps a game-level payload as a RELIABLE_ORDERED encapsulated packet inside a DATA datagram.
     * Encapsulated header: flags(1) + bitLength(2 BE) + msgSeq(3 LE) + orderIndex(3 LE) + orderChannel(1).
     */
    public ByteBuf buildReliableOrderedDatagram(ByteBufAllocator alloc, ByteBuf gamePayload) {
        int msgSeq = outMessageSeq.getAndIncrement();

        ByteBuf encap = alloc.buffer(10 + gamePayload.readableBytes());
        encap.writeByte(0x60);                              // RELIABLE_ORDERED reliability flags
        encap.writeShort(gamePayload.readableBytes() * 8);  // bit length (BE)
        writeInt24LE(encap, msgSeq);                        // message sequence
        writeInt24LE(encap, msgSeq);                        // order index (same as msg seq for single channel)
        encap.writeByte(0);                                 // order channel 0
        encap.writeBytes(gamePayload);

        ByteBuf datagram = wrapDatagramReliable(alloc, encap, true);
        encap.release();
        return datagram;
    }

    /** Retransmits any pending datagrams that have exceeded the timeout. Call periodically. */
    public void retransmitTimedOut(ChannelHandlerContext ctx) {
        long now = System.currentTimeMillis();
        for (java.util.Map.Entry<Integer, PendingPacket> entry : pending.entrySet()) {
            if (now - entry.getValue().sentMs > RETRANSMIT_TIMEOUT_MS) {
                logger.debug("RakNet retransmit timed-out seq={}", entry.getKey());
                ctx.writeAndFlush(entry.getValue().payload.retainedDuplicate());
            }
        }
    }

    /** Releases all pending outbound buffers. Call on connection close. */
    public void close() {
        for (PendingPacket pp : pending.values()) pp.payload.release();
        pending.clear();
    }

    // ── Private helpers ──────────────────────────────────────────────────────

    private void sendAck(ChannelHandlerContext ctx, int seqStart, int seqEnd) {
        ByteBufAllocator alloc = ctx.alloc();
        ByteBuf ack = alloc.buffer(8);
        ack.writeByte(PacketConstants.RAKNET_ACK);
        ack.writeShort(1);               // one record
        ack.writeByte(seqStart == seqEnd ? 0x01 : 0x00); // single=1, range=0 (Bedrock convention)
        writeInt24LE(ack, seqStart);
        if (seqStart != seqEnd) writeInt24LE(ack, seqEnd);
        ctx.writeAndFlush(ack);
    }

    private void sendNak(ChannelHandlerContext ctx, int seqStart, int seqEnd) {
        ByteBufAllocator alloc = ctx.alloc();
        ByteBuf nak = alloc.buffer(8);
        nak.writeByte(PacketConstants.RAKNET_NAK);
        nak.writeShort(1);
        nak.writeByte(seqStart == seqEnd ? 0x01 : 0x00);
        writeInt24LE(nak, seqStart);
        if (seqStart != seqEnd) writeInt24LE(nak, seqEnd);
        ctx.writeAndFlush(nak);
        logger.debug("RakNet NAK sent: seq {}-{}", seqStart, seqEnd);
    }

    static int readInt24LE(ByteBuf buf) {
        return (buf.readByte() & 0xFF)
             | ((buf.readByte() & 0xFF) << 8)
             | ((buf.readByte() & 0xFF) << 16);
    }

    static void writeInt24LE(ByteBuf buf, int value) {
        buf.writeByte(value & 0xFF);
        buf.writeByte((value >> 8) & 0xFF);
        buf.writeByte((value >> 16) & 0xFF);
    }
}
