package com.overmind.api;

import java.util.Random;

/**
 * 2D Perlin noise for terrain height generation.
 *
 * <p>{@link #noise} returns a value in [-1, 1].
 * {@link #octaveNoise} sums multiple octaves (fBm) for natural-looking terrain.
 */
public class SimplexNoise {

    private final int[] p = new int[512];

    public SimplexNoise(long seed) {
        Random rng = new Random(seed);
        int[] src = new int[256];
        for (int i = 0; i < 256; i++) src[i] = i;
        for (int i = 255; i > 0; i--) {
            int j = rng.nextInt(i + 1);
            int t = src[i]; src[i] = src[j]; src[j] = t;
        }
        for (int i = 0; i < 512; i++) p[i] = src[i & 255];
    }

    private static double fade(double t) { return t * t * t * (t * (t * 6 - 15) + 10); }
    private static double lerp(double a, double b, double t) { return a + t * (b - a); }

    private double grad(int hash, double x, double y) {
        int h = hash & 7;
        double u = h < 4 ? x : y;
        double v = h < 4 ? y : x;
        return ((h & 1) == 0 ? u : -u) + ((h & 2) == 0 ? v : -v);
    }

    /** Returns noise in [-1, 1]. */
    public double noise(double x, double y) {
        int xi = (int) Math.floor(x) & 255;
        int yi = (int) Math.floor(y) & 255;
        double xf = x - Math.floor(x);
        double yf = y - Math.floor(y);
        double u = fade(xf), v = fade(yf);
        int aa = p[p[xi    ] + yi    ];
        int ab = p[p[xi    ] + yi + 1];
        int ba = p[p[xi + 1] + yi    ];
        int bb = p[p[xi + 1] + yi + 1];
        return lerp(
                lerp(grad(aa, xf,     yf    ), grad(ba, xf - 1, yf    ), u),
                lerp(grad(ab, xf,     yf - 1), grad(bb, xf - 1, yf - 1), u),
                v);
    }

    /**
     * Fractional Brownian motion over multiple octaves.
     *
     * @param octaves     number of octaves (5–6 gives natural-looking terrain)
     * @param scale       base frequency multiplier
     * @param persistence amplitude decay per octave (0.5 is typical)
     * @param lacunarity  frequency growth per octave (2.0 is typical)
     */
    public double octaveNoise(double x, double y,
                              int octaves, double scale,
                              double persistence, double lacunarity) {
        double value = 0, amplitude = 1, frequency = scale, max = 0;
        for (int i = 0; i < octaves; i++) {
            value     += noise(x * frequency, y * frequency) * amplitude;
            max       += amplitude;
            amplitude *= persistence;
            frequency *= lacunarity;
        }
        return value / max;
    }
}
