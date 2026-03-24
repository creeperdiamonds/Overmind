package com.overmind.java;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.HashMap;
import java.util.Map;

/**
 * Reassembles RakNet split (fragmented) packets.
 *
 * <h3>RakNet split-packet format</h3>
 * When a RakNet encapsulated packet is too large for one datagram it is split. Each fragment
 * carries three extra header fields (present only when the split flag is set in the encapsulated
 * packet's reliability byte):
 * <pre>
 *   int    split_count   (BE) — total number of fragments
 *   short  compound_id   (BE) — identifies this split-packet stream; wraps around at 65535
 *   int    split_index   (BE) — zero-based index of this fragment
 * </pre>
 *
 * <h3>Usage</h3>
 * <pre>{@code
 * FragmentReassembler reassembler = new FragmentReassembler(ctx.alloc());
 *
 * // Inside the encapsulated-packet loop:
 * if (isSplit) {
 *     ByteBuf complete = reassembler.add(compoundId, splitCount, splitIndex, fragment);
 *     if (complete != null) {
 *         handleGamePacket(complete); // fully reassembled
 *         complete.release();
 *     }
 * } else {
 *     handleGamePacket(fragment);
 * }
 * }</pre>
 *
 * <p>Incomplete streams are kept indefinitely. Call {@link #close()} when the connection ends to
 * release all pooled buffers.
 */
public class FragmentReassembler {
    private static final Logger logger = LoggerFactory.getLogger(FragmentReassembler.class);

    /** Maximum number of concurrent split-packet streams before the oldest is evicted. */
    private static final int MAX_STREAMS = 32;

    private static final class Stream {
        final int       totalCount;
        final ByteBuf[] fragments;
        int             received;

        Stream(int totalCount, ByteBufAllocator alloc) {
            this.totalCount = totalCount;
            this.fragments  = new ByteBuf[totalCount];
            this.received   = 0;
        }

        boolean complete() {
            return received == totalCount;
        }

        void release() {
            for (ByteBuf f : fragments) {
                if (f != null) f.release();
            }
        }
    }

    private final ByteBufAllocator alloc;
    // compound_id (0–65535) → reassembly stream
    private final Map<Integer, Stream> streams = new HashMap<>();

    public FragmentReassembler(ByteBufAllocator alloc) {
        this.alloc = alloc;
    }

    /**
     * Adds one fragment and returns the fully reassembled buffer when all fragments have arrived,
     * or {@code null} if more fragments are still outstanding.
     *
     * <p>The returned buffer has a ref-count of 1; the caller is responsible for releasing it.
     * The {@code fragment} buffer is retained internally; do NOT release it after calling this.
     *
     * @param compoundId  RakNet compound_id (identifies the split-packet stream)
     * @param splitCount  total fragment count for this stream
     * @param splitIndex  zero-based index of this fragment (0 … splitCount−1)
     * @param fragment    payload bytes for this fragment (caller must not release after this call)
     * @return fully reassembled buffer, or {@code null} if assembly is incomplete
     */
    public ByteBuf add(int compoundId, int splitCount, int splitIndex, ByteBuf fragment) {
        if (splitIndex < 0 || splitIndex >= splitCount) {
            logger.warn("FragmentReassembler: invalid splitIndex {} / {} (compound {})",
                    splitIndex, splitCount, compoundId);
            fragment.release();
            return null;
        }

        Stream stream = streams.computeIfAbsent(compoundId, id -> {
            if (streams.size() >= MAX_STREAMS) evictOldest();
            return new Stream(splitCount, alloc);
        });

        if (stream.fragments[splitIndex] != null) {
            logger.warn("FragmentReassembler: duplicate fragment index {} for compound {}",
                    splitIndex, compoundId);
            fragment.release();
            return null;
        }

        stream.fragments[splitIndex] = fragment.retain();
        stream.received++;

        if (!stream.complete()) return null;

        // All fragments received — concatenate in order
        streams.remove(compoundId);
        int totalBytes = 0;
        for (ByteBuf f : stream.fragments) totalBytes += f.readableBytes();

        ByteBuf assembled = alloc.buffer(totalBytes);
        for (ByteBuf f : stream.fragments) {
            assembled.writeBytes(f);
            f.release();
        }

        logger.debug("FragmentReassembler: reassembled compound {} ({} bytes from {} fragments)",
                compoundId, totalBytes, splitCount);
        return assembled;
    }

    /** Releases all in-flight fragment buffers. Call when the connection closes. */
    public void close() {
        for (Stream s : streams.values()) s.release();
        streams.clear();
    }

    private void evictOldest() {
        Integer oldest = streams.keySet().iterator().next();
        logger.warn("FragmentReassembler: evicting incomplete stream compound={} (MAX_STREAMS reached)", oldest);
        streams.remove(oldest).release();
    }
}
