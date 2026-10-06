//go:build cgo

/*
 * The C half of package vpx: a thin wrapper around the libvpx VP8 encoder and
 * the BGRA -> I420 conversion (with optional downscaling) that feeds it. Plain
 * C only: nothing here may pull in a C++ runtime.
 *
 * Image planes are Go memory, passed in for the duration of one call and
 * never kept.
 */
#ifndef FREEDESK_FDVPX_H
#define FREEDESK_FDVPX_H

#include <stddef.h>
#include <stdint.h>

typedef struct {
	int width, height; /* the encoded size, even */
	int target_kbps;
	int buf_ms, buf_initial_ms, buf_optimal_ms;
	int undershoot_pct, overshoot_pct;
	int max_intra_pct;
	int kf_max_dist;
	int cpu_used;
	int threads;
	int error_resilient;
	int timebase_den; /* pts units per second */
} fd_vpx_settings;

typedef struct fd_vpx fd_vpx;

/* Opens an encoder; on failure returns NULL with a message in err. */
fd_vpx *fd_vpx_open(const fd_vpx_settings *s, char *err, int errlen);

/* Encodes one I420 image of the encoder's size. On success returns 0 and
 * points data at the encoded frame (valid until the next call; size 0 if
 * libvpx produced none). */
int fd_vpx_encode(fd_vpx *e, const uint8_t *y, const uint8_t *u, const uint8_t *v,
                  int y_stride, int uv_stride, int64_t pts, unsigned long duration,
                  int force_keyframe, const uint8_t **data, size_t *size, int *keyframe,
                  char *err, int errlen);

void fd_vpx_close(fd_vpx *e);

/* convert.c */
typedef struct fd_scaler fd_scaler;

fd_scaler *fd_scaler_new(int src_w, int src_h, int dst_w, int dst_h);
void fd_scaler_free(fd_scaler *s);
/* Area-averages a src_w x src_h BGRA frame down to dst_w x dst_h BGRA
 * (dst stride dst_w*4). */
void fd_scale_bgra(fd_scaler *s, const uint8_t *src, int stride, uint8_t *dst);
/* Converts width x height (both even) BGRA pixels to I420. */
void fd_bgra_to_i420(const uint8_t *src, int stride, int width, int height,
                     uint8_t *y, int y_stride, uint8_t *u, int u_stride,
                     uint8_t *v, int v_stride);

#endif
