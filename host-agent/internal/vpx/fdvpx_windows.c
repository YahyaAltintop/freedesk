//go:build cgo

/*
 * The libvpx VP8 encoder, configured from fd_vpx_settings. The settings are
 * derived in Go (rateControlFor) from what ffmpeg used to set; this file only
 * applies them, in the order ffmpeg applied them.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <vpx/vp8cx.h>
#include <vpx/vpx_encoder.h>

#include "fdvpx.h"

struct fd_vpx {
	vpx_codec_ctx_t codec;
	int codec_open;
	int w, h;
};

static void codec_error(fd_vpx *e, const char *what, char *err, int errlen) {
	const char *detail = vpx_codec_error_detail(&e->codec);
	snprintf(err, (size_t)errlen, "%s: %s%s%s", what, vpx_codec_error(&e->codec),
	         detail ? ": " : "", detail ? detail : "");
}

fd_vpx *fd_vpx_open(const fd_vpx_settings *s, char *err, int errlen) {
	if (s->width <= 0 || s->height <= 0 || (s->width & 1) || (s->height & 1)) {
		snprintf(err, (size_t)errlen, "cannot encode a %dx%d frame", s->width, s->height);
		return NULL;
	}
	fd_vpx *e = calloc(1, sizeof *e);
	if (!e) {
		snprintf(err, (size_t)errlen, "out of memory");
		return NULL;
	}
	e->w = s->width;
	e->h = s->height;

	vpx_codec_enc_cfg_t cfg;
	vpx_codec_err_t res = vpx_codec_enc_config_default(vpx_codec_vp8_cx(), &cfg, 0);
	if (res != VPX_CODEC_OK) {
		snprintf(err, (size_t)errlen, "default config: %s", vpx_codec_err_to_string(res));
		goto fail;
	}
	cfg.g_w = (unsigned)s->width;
	cfg.g_h = (unsigned)s->height;
	cfg.g_timebase.num = 1;
	cfg.g_timebase.den = s->timebase_den;
	cfg.g_threads = (unsigned)s->threads;
	cfg.g_lag_in_frames = 0;
	cfg.g_pass = VPX_RC_ONE_PASS;
	cfg.g_error_resilient = s->error_resilient ? VPX_ERROR_RESILIENT_DEFAULT : 0;
	cfg.rc_end_usage = VPX_CBR;
	cfg.rc_target_bitrate = (unsigned)s->target_kbps;
	cfg.rc_dropframe_thresh = 0;
	cfg.rc_buf_sz = (unsigned)s->buf_ms;
	cfg.rc_buf_initial_sz = (unsigned)s->buf_initial_ms;
	cfg.rc_buf_optimal_sz = (unsigned)s->buf_optimal_ms;
	cfg.rc_undershoot_pct = (unsigned)s->undershoot_pct;
	cfg.rc_overshoot_pct = (unsigned)s->overshoot_pct;
	cfg.kf_mode = VPX_KF_AUTO;
	cfg.kf_max_dist = (unsigned)s->kf_max_dist;

	res = vpx_codec_enc_init(&e->codec, vpx_codec_vp8_cx(), &cfg, 0);
	if (res != VPX_CODEC_OK) {
		codec_error(e, "init", err, errlen);
		goto fail;
	}
	e->codec_open = 1;

	/* ffmpeg treated a failed control as a warning; here it is an error, so a
	 * library that does not take a setting cannot silently encode otherwise. */
	if (vpx_codec_control(&e->codec, VP8E_SET_CPUUSED, s->cpu_used) != VPX_CODEC_OK ||
	    vpx_codec_control(&e->codec, VP8E_SET_NOISE_SENSITIVITY, 0) != VPX_CODEC_OK ||
	    vpx_codec_control(&e->codec, VP8E_SET_TOKEN_PARTITIONS, VP8_ONE_TOKENPARTITION) != VPX_CODEC_OK ||
	    vpx_codec_control(&e->codec, VP8E_SET_STATIC_THRESHOLD, 0) != VPX_CODEC_OK ||
	    vpx_codec_control(&e->codec, VP8E_SET_MAX_INTRA_BITRATE_PCT, s->max_intra_pct) != VPX_CODEC_OK) {
		codec_error(e, "control", err, errlen);
		goto fail;
	}
	return e;

fail:
	fd_vpx_close(e);
	return NULL;
}

int fd_vpx_encode(fd_vpx *e, const uint8_t *y, const uint8_t *u, const uint8_t *v,
                  int y_stride, int uv_stride, int64_t pts, unsigned long duration,
                  int force_keyframe, const uint8_t **data, size_t *size, int *keyframe,
                  char *err, int errlen) {
	*data = NULL;
	*size = 0;
	*keyframe = 0;

	/* An image header over the caller's planes, for this call only: libvpx
	 * copies the picture into its own buffers before vpx_codec_encode
	 * returns (no lag), so nothing here outlives the call. */
	vpx_image_t img;
	memset(&img, 0, sizeof img);
	if (!vpx_img_wrap(&img, VPX_IMG_FMT_I420, (unsigned)e->w, (unsigned)e->h, 1, (unsigned char *)y)) {
		snprintf(err, (size_t)errlen, "cannot describe a %dx%d image", e->w, e->h);
		return -1;
	}
	img.planes[VPX_PLANE_Y] = (unsigned char *)y;
	img.planes[VPX_PLANE_U] = (unsigned char *)u;
	img.planes[VPX_PLANE_V] = (unsigned char *)v;
	img.stride[VPX_PLANE_Y] = y_stride;
	img.stride[VPX_PLANE_U] = uv_stride;
	img.stride[VPX_PLANE_V] = uv_stride;

	vpx_enc_frame_flags_t flags = force_keyframe ? VPX_EFLAG_FORCE_KF : 0;
	if (vpx_codec_encode(&e->codec, &img, pts, duration, flags, VPX_DL_REALTIME) != VPX_CODEC_OK) {
		codec_error(e, "encode", err, errlen);
		return -1;
	}
	/* Realtime VP8 with no lag hands back exactly one frame per input. The
	 * packet stays valid until the next vpx_codec_encode. */
	int frames = 0;
	vpx_codec_iter_t iter = NULL;
	const vpx_codec_cx_pkt_t *pkt;
	while ((pkt = vpx_codec_get_cx_data(&e->codec, &iter)) != NULL) {
		if (pkt->kind != VPX_CODEC_CX_FRAME_PKT) {
			continue;
		}
		frames++;
		*data = pkt->data.frame.buf;
		*size = pkt->data.frame.sz;
		*keyframe = (pkt->data.frame.flags & VPX_FRAME_IS_KEY) != 0;
	}
	if (frames > 1) {
		snprintf(err, (size_t)errlen, "encode: %d frames for one input", frames);
		return -1;
	}
	return 0;
}

void fd_vpx_close(fd_vpx *e) {
	if (!e) {
		return;
	}
	if (e->codec_open) {
		vpx_codec_destroy(&e->codec);
	}
	free(e);
}
