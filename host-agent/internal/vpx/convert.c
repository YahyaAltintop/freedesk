//go:build cgo

/*
 * BGRA -> I420 conversion and area-averaging downscale, in portable C.
 *
 * The colour matrix is BT.601 with limited ("TV") range: the one ffmpeg's
 * swscale applied when the agent asked it for yuv420p from the desktop's RGB,
 * and the one a VP8 decoder assumes. Chroma is the average of each 2x2 block.
 */
#include <stdlib.h>
#include <string.h>

#include "fdvpx.h"

/* (66, 129, 25)/256 etc. are the BT.601 limited-range coefficients scaled to
 * eight bits; 0x1080 adds the +16 offset and rounding, 0x8080 the +128 offset
 * and rounding. Every result is within [16, 240] for any 8-bit input, so no
 * clamping is needed. */
#define FD_Y(r, g, b) ((uint8_t)((66 * (r) + 129 * (g) + 25 * (b) + 0x1080) >> 8))
#define FD_U(r, g, b) ((uint8_t)((112 * (b) - 74 * (g) - 38 * (r) + 0x8080) >> 8))
#define FD_V(r, g, b) ((uint8_t)((112 * (r) - 94 * (g) - 18 * (b) + 0x8080) >> 8))

void fd_bgra_to_i420(const uint8_t *src, int stride, int width, int height,
                     uint8_t *y, int y_stride, uint8_t *u, int u_stride,
                     uint8_t *v, int v_stride) {
	for (int row = 0; row < height; row += 2) {
		const uint8_t *s0 = src + (size_t)row * stride;
		const uint8_t *s1 = s0 + stride;
		uint8_t *y0 = y + (size_t)row * y_stride;
		uint8_t *y1 = y0 + y_stride;
		uint8_t *ur = u + (size_t)(row / 2) * u_stride;
		uint8_t *vr = v + (size_t)(row / 2) * v_stride;
		for (int x = 0; x < width; x += 2) {
			const uint8_t *a = s0 + (size_t)x * 4;
			const uint8_t *b = s1 + (size_t)x * 4;
			int b00 = a[0], g00 = a[1], r00 = a[2];
			int b01 = a[4], g01 = a[5], r01 = a[6];
			int b10 = b[0], g10 = b[1], r10 = b[2];
			int b11 = b[4], g11 = b[5], r11 = b[6];
			y0[x] = FD_Y(r00, g00, b00);
			y0[x + 1] = FD_Y(r01, g01, b01);
			y1[x] = FD_Y(r10, g10, b10);
			y1[x + 1] = FD_Y(r11, g11, b11);
			int bb = (b00 + b01 + b10 + b11 + 2) >> 2;
			int gg = (g00 + g01 + g10 + g11 + 2) >> 2;
			int rr = (r00 + r01 + r10 + r11 + 2) >> 2;
			ur[x / 2] = FD_U(rr, gg, bb);
			vr[x / 2] = FD_V(rr, gg, bb);
		}
	}
}

/* One axis of the scaler. Output pixel i covers the source interval
 * [i*src/dst, (i+1)*src/dst); each source pixel it touches weighs its overlap
 * with that interval, in 1/256ths, and the weights of one output pixel sum to
 * exactly 256. Every output pixel has the same number of taps (the most any
 * of them needs), so the loops over them have a fixed trip count; the taps a
 * pixel does not need weigh 0. */
typedef struct {
	int taps;
	int *first;       /* dst entries: the first source pixel */
	uint16_t *weight; /* dst * taps entries */
} fd_axis;

struct fd_scaler {
	int src_w, src_h, dst_w, dst_h;
	fd_axis x, y;
	/* One row of vertically weighted B, G, R, A: src_w * 4 entries plus
	 * x.taps zeroed pixels of padding, which the unused taps of the last
	 * output pixels read (with weight 0) instead of running off the end. */
	uint16_t *acc;
};

/* Measured in units of 1/dst source pixels, output pixel i is
 * [i*src, (i+1)*src) and source pixel k is [k*dst, (k+1)*dst): exact integer
 * overlaps, with the rounding folded into the largest weight. */
static int build_axis(int src, int dst, fd_axis *ax) {
	if (src <= 0 || dst <= 0) {
		return -1;
	}
	int taps = 1;
	for (int i = 0; i < dst; i++) {
		int64_t lo = (int64_t)i * src, hi = (int64_t)(i + 1) * src;
		int count = (int)((hi - 1) / dst) - (int)(lo / dst) + 1;
		if (count > taps) {
			taps = count;
		}
	}
	ax->taps = taps;
	ax->first = calloc((size_t)dst, sizeof *ax->first);
	ax->weight = calloc((size_t)dst * (size_t)taps, sizeof *ax->weight);
	if (!ax->first || !ax->weight) {
		return -1;
	}
	for (int i = 0; i < dst; i++) {
		int64_t lo = (int64_t)i * src, hi = (int64_t)(i + 1) * src;
		int first = (int)(lo / dst);
		int count = (int)((hi - 1) / dst) - first + 1;
		uint16_t *w = ax->weight + (size_t)i * (size_t)taps;
		int sum = 0, big = 0;
		for (int k = 0; k < count; k++) {
			int64_t a = (int64_t)(first + k) * dst, b = a + dst;
			int64_t o = (b < hi ? b : hi) - (a > lo ? a : lo);
			int wk = (int)((o * 256 + src / 2) / src);
			w[k] = (uint16_t)wk;
			sum += wk;
			if (wk > w[big]) {
				big = k;
			}
		}
		w[big] = (uint16_t)((int)w[big] + 256 - sum);
		ax->first[i] = first;
	}
	return 0;
}

fd_scaler *fd_scaler_new(int src_w, int src_h, int dst_w, int dst_h) {
	fd_scaler *s = calloc(1, sizeof *s);
	if (!s) {
		return NULL;
	}
	s->src_w = src_w;
	s->src_h = src_h;
	s->dst_w = dst_w;
	s->dst_h = dst_h;
	if (build_axis(src_w, dst_w, &s->x) != 0 || build_axis(src_h, dst_h, &s->y) != 0) {
		fd_scaler_free(s);
		return NULL;
	}
	s->acc = calloc(((size_t)src_w + (size_t)s->x.taps) * 4, sizeof *s->acc);
	if (!s->acc) {
		fd_scaler_free(s);
		return NULL;
	}
	return s;
}

void fd_scaler_free(fd_scaler *s) {
	if (!s) {
		return;
	}
	free(s->x.first);
	free(s->x.weight);
	free(s->y.first);
	free(s->y.weight);
	free(s->acc);
	free(s);
}

/* The horizontal pass for a fixed number of taps T, so the compiler can
 * unroll it. acc holds the vertically weighted row (each channel at most
 * 255*256); the result is weighted again by at most 256 and fits 32 bits. */
#define FD_HPASS(T)                                                         \
	for (int ox = 0; ox < dst_w; ox++) {                                    \
		const uint16_t *a = acc + 4 * (size_t)first[ox];                     \
		const uint16_t *w = weight + (size_t)ox * (T);                       \
		uint32_t b = 0, g = 0, r = 0;                                       \
		for (int k = 0; k < (T); k++) {                                     \
			uint32_t wk = w[k];                                             \
			b += a[4 * k] * wk;                                             \
			g += a[4 * k + 1] * wk;                                         \
			r += a[4 * k + 2] * wk;                                         \
		}                                                                   \
		out[4 * ox] = (uint8_t)((b + 32768) >> 16);                         \
		out[4 * ox + 1] = (uint8_t)((g + 32768) >> 16);                     \
		out[4 * ox + 2] = (uint8_t)((r + 32768) >> 16);                     \
		out[4 * ox + 3] = 255;                                              \
	}

void fd_scale_bgra(fd_scaler *s, const uint8_t *src, int stride, uint8_t *dst) {
	const int n = s->src_w * 4;
	const int dst_w = s->dst_w;
	const int *first = s->x.first;
	const uint16_t *weight = s->x.weight;
	uint16_t *acc = s->acc;
	for (int oy = 0; oy < s->dst_h; oy++) {
		/* Vertical pass over whole rows: contiguous bytes, which the compiler
		 * turns into SIMD multiply-adds. Weights sum to 256, so a channel
		 * peaks at 255*256 and fits 16 bits. */
		const int top = s->y.first[oy];
		const uint16_t *wy = s->y.weight + (size_t)oy * (size_t)s->y.taps;
		const uint8_t *row = src + (size_t)top * (size_t)stride;
		const uint16_t w0 = wy[0];
		for (int i = 0; i < n; i++) {
			acc[i] = (uint16_t)(row[i] * w0);
		}
		for (int k = 1; k < s->y.taps; k++) {
			const uint16_t wk = wy[k];
			if (wk == 0) {
				continue;
			}
			row = src + (size_t)(top + k) * (size_t)stride;
			for (int i = 0; i < n; i++) {
				acc[i] = (uint16_t)(acc[i] + row[i] * wk);
			}
		}
		uint8_t *out = dst + (size_t)oy * (size_t)dst_w * 4;
		switch (s->x.taps) {
		case 1:
			FD_HPASS(1)
			break;
		case 2:
			FD_HPASS(2)
			break;
		case 3:
			FD_HPASS(3)
			break;
		case 4:
			FD_HPASS(4)
			break;
		default:
			FD_HPASS(s->x.taps)
			break;
		}
	}
}
