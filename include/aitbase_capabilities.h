/* Generated from the device's capability registry. Do not edit. */
#ifndef AITBASE_CAPABILITIES_H
#define AITBASE_CAPABILITIES_H

/*
 * Device status codes, in aitbase_event_t.status of an AITBASE_EVENT_ERROR. The stable
 * reason is in aitbase_event_t.detail (docs/capabilities.md lists every code).
 */
#define AITBASE_DEVICE_INVALID (-1) /* The request or its arguments are invalid. */
#define AITBASE_DEVICE_STALE (-2) /* No screen is open, or the screen is no longer current. */
#define AITBASE_DEVICE_BUSY (-3) /* The device can't take this request now; try again later. */
#define AITBASE_DEVICE_NO_MEMORY (-4) /* The device ran out of memory for this request. */
#define AITBASE_DEVICE_DENIED (-5) /* The plugin may not do this: a missing grant, or a destination the device refuses. */
#define AITBASE_DEVICE_UNAVAILABLE (-6) /* Not available now or on this device: no SD card, or the screen isn't visible. */
#define AITBASE_DEVICE_LIMIT (-7) /* A rate, size or storage limit was reached. */
#define AITBASE_DEVICE_FAILED (-8) /* The device tried and failed: network error, timeout, TLS error. */

/* Capabilities a plugin can request in plugin.json (requested_capabilities). */
#define AITBASE_CAP_UI "ui"
#define AITBASE_CAP_STORAGE "storage"
#define AITBASE_CAP_HTTP "http"
#define AITBASE_CAP_AUDIO_OUTPUT "audio.output"
#define AITBASE_CAP_SCHEDULE "schedule"
#define AITBASE_CAP_SETTINGS "settings"
#define AITBASE_CAP_STATUS "status"
#define AITBASE_CAP_IDLE_FACE "idle.face"
#define AITBASE_CAP_AUDIO_STREAM "audio.stream"
#define AITBASE_CAP_AUDIO_BACKGROUND "audio.background"
#define AITBASE_CAP_HTTP_STREAM "http.stream"

/* Device limits. */
#define AITBASE_LIMIT_RX_DEFAULT 4096u
#define AITBASE_LIMIT_RX_MAX 16384u
#define AITBASE_LIMIT_STORAGE_KEY_MAX 64u
#define AITBASE_LIMIT_STORAGE_VALUE_MAX 4096u
#define AITBASE_LIMIT_STORAGE_KEYS_MAX 128u
#define AITBASE_LIMIT_STORAGE_TOTAL_MAX 65536u
#define AITBASE_LIMIT_HTTP_URL_MAX 1024u
#define AITBASE_LIMIT_HTTP_HEADERS_MAX 16u
#define AITBASE_LIMIT_HTTP_HEADER_MAX 512u
#define AITBASE_LIMIT_HTTP_BODY_MAX 8192u
#define AITBASE_LIMIT_HTTP_RESPONSE_MAX 65536u
#define AITBASE_LIMIT_HTTP_TIMEOUT_MS_DEFAULT 10000u
#define AITBASE_LIMIT_HTTP_TIMEOUT_MS_MAX 15000u
#define AITBASE_LIMIT_AUDIO_SOUND_BYTES_MAX 327680u
#define AITBASE_LIMIT_AUDIO_SAMPLE_RATE 16000u
#define AITBASE_LIMIT_AUDIO_STREAM_FRAME_MAX 8192u
#define AITBASE_LIMIT_AUDIO_STREAM_QUEUE_MS 200u
#define AITBASE_LIMIT_AUDIO_STREAM_IDLE_S 10u
#define AITBASE_LIMIT_AUDIO_STREAM_FD 4u
#define AITBASE_LIMIT_HTTP_STREAM_FD 5u
#define AITBASE_LIMIT_HTTP_STREAM_FRAME_MAX 8192u
#define AITBASE_LIMIT_HTTP_STREAM_IDLE_S 10u
#define AITBASE_LIMIT_SCHEDULE_ENTRIES_MAX 8u
#define AITBASE_LIMIT_SCHEDULE_AHEAD_S_MAX 31622400u
#define AITBASE_LIMIT_SCHEDULE_LATE_S_MAX 300u
#define AITBASE_LIMIT_SCHEDULE_COMMUNITY_ENTRIES_MAX 4u
#define AITBASE_LIMIT_SCHEDULE_COMMUNITY_SPACING_S 300u
#define AITBASE_LIMIT_SCHEDULE_COMMUNITY_RING_S 60u
#define AITBASE_LIMIT_SETTINGS_KEYS_MAX 64u
#define AITBASE_LIMIT_SETTINGS_KEY_MAX 32u
#define AITBASE_LIMIT_SETTINGS_VALUE_MAX 512u

#endif /* AITBASE_CAPABILITIES_H */
