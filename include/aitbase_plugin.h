#ifndef AITBASE_PLUGIN_H
#define AITBASE_PLUGIN_H

/*
 * AITBASE VISION plugin API (C), API version 1 — preview, may still change before SDK 1.0.
 *
 * A plugin is a set of callbacks. The runtime owns the loop: it starts the plugin, delivers one
 * event at a time to aitbase_on_event() and returns when the plugin stops. Callbacks must return
 * promptly; nothing in this API blocks. This is the model the WASM runtime uses, so plugins written
 * against it run unchanged when they are compiled to WebAssembly.
 *
 * A plugin owns at most one screen. The device owns rendering, focus and the exit gesture: LEFT
 * moves focus, RIGHT activates the focused widget, LEFT long press always leaves. The device may
 * take the screen back at any time (voice assistant, settings...), reported as
 * AITBASE_EVENT_PREEMPTED.
 *
 * Requests return once they are queued; failures arrive later as AITBASE_EVENT_ERROR.
 * Screen functions return 0 on success or a negative aitbase_status_t. Storage, HTTP and sound
 * functions return a request id (> 0) instead; their results arrive as AITBASE_EVENT_RESULT or
 * AITBASE_EVENT_SOUND_DONE carrying the same id in event->request, and failures as
 * AITBASE_EVENT_ERROR with that id too.
 *
 * Storage, HTTP and sound need the capability in plugin.json's requested_capabilities ("storage",
 * "http", "audio.output"); without it the request fails with detail "no_grant". The device's
 * limits and every error code are in docs/capabilities.md and aitbase_capabilities.h.
 *
 * Minimal plugin:
 *
 *   #include "aitbase_plugin.h"
 *   AITBASE_PLUGIN_ID("example.hello");
 *   int aitbase_on_start(aitbase_plugin_t *p) { return aitbase_ui_open(p, "hello", 0); }
 *   void aitbase_on_event(aitbase_plugin_t *p, const aitbase_event_t *ev) {
 *       if (ev->kind == AITBASE_EVENT_PREEMPTED) aitbase_plugin_stop(p);
 *   }
 */

#include <stddef.h>
#include <stdint.h>

#include "aitbase_capabilities.h"

#ifdef __cplusplus
extern "C" {
#endif

#define AITBASE_PLUGIN_API_VERSION 1

/* Longest wait accepted by aitbase_ui_open(); larger values are clamped. */
#define AITBASE_UI_MAX_WAIT_MS 30000u

typedef enum aitbase_status {
    AITBASE_OK = 0,
    AITBASE_ERR_ARGUMENT = -1,  /* NULL, malformed or oversized argument */
    AITBASE_ERR_CONNECT = -2,   /* the device is unavailable */
    AITBASE_ERR_IO = -3,        /* connection to the device lost */
    AITBASE_ERR_PROTOCOL = -4,  /* the device speaks an unsupported protocol */
    AITBASE_ERR_NO_MEMORY = -5,
    AITBASE_ERR_NOT_FOUND = -6, /* no compiled screen with that name in the plugin package */
    AITBASE_ERR_TIMEOUT = -7,     /* aitbase_call(): no reply in time. Since SDK 0.10 */
    AITBASE_ERR_UNSUPPORTED = -8, /* not available in this runtime (aitbase_call() in WASM). Since SDK 0.10 */
} aitbase_status_t;

typedef enum aitbase_event_kind {
    AITBASE_EVENT_NONE = 0,
    AITBASE_EVENT_OPENED = 1,          /* the screen is visible */
    AITBASE_EVENT_REJECTED = 2,        /* open refused, cancelled or timed out; see detail */
    AITBASE_EVENT_CLOSED = 3,          /* closed by aitbase_ui_close() */
    AITBASE_EVENT_PREEMPTED = 4,       /* the device took the screen back */
    AITBASE_EVENT_ACTION = 5,          /* the user activated node; action is its registered action */
    AITBASE_EVENT_UPDATE_REJECTED = 6, /* an update to node was refused; see detail */
    AITBASE_EVENT_ERROR = 7,           /* a request failed; op names it, status/detail say why */
    AITBASE_EVENT_TIMER = 8,           /* the interval set with aitbase_timer_set() elapsed */
    AITBASE_EVENT_RESULT = 9,          /* a storage or HTTP request succeeded; see request, op, text, code */
    AITBASE_EVENT_SOUND_DONE = 10,     /* the sound started by request ended; detail is "finished",
                                          "stopped" or "failed" */
    AITBASE_EVENT_KEY = 11,            /* a button was pressed on the plugin's screen; see key, gesture.
                                          Since SDK 0.8 */
    AITBASE_EVENT_DEVICE = 12,         /* an event this SDK has no kind for: op is "<cap>.<event>",
                                          e.g. "schedule.due"; text is its data as a JSON object.
                                          Since SDK 0.10 */
} aitbase_event_kind_t;

/* The device's buttons, in AITBASE_EVENT_KEY. */
typedef enum aitbase_key {
    AITBASE_KEY_NONE = 0,
    AITBASE_KEY_LEFT = 1,
    AITBASE_KEY_RIGHT = 2,
} aitbase_key_t;

/* How a button was pressed. A long press always comes after a press of the same button: the
 * device can't know at press-down that the user will hold it. LEFT long press always leaves the
 * plugin and is never delivered; RIGHT long press is. */
typedef enum aitbase_gesture {
    AITBASE_GESTURE_NONE = 0,
    AITBASE_GESTURE_PRESS = 1,
    AITBASE_GESTURE_LONG_PRESS = 2,
} aitbase_gesture_t;

typedef struct aitbase_event {
    aitbase_event_kind_t kind;
    char node[33];
    char action[33];
    char op[16];       /* ERROR, RESULT: the request's operation: "open", "update", "close",
                          "storage.get", "http.request", "audio.play"... */
    int status;        /* ERROR: a device status, AITBASE_DEVICE_* */
    char detail[65];   /* ERROR: a stable error code; SOUND_DONE: how the sound ended */
    /* Since SDK 0.5. */
    uint32_t request;  /* ERROR, RESULT, SOUND_DONE: the id the request function returned; 0 for
                          screen requests */
    const char *text;  /* RESULT: storage.get: the value; storage.keys: the keys, one per line;
                          http.request: the response body, text only. DEVICE: the event's data
                          as JSON. "" otherwise. Valid until aitbase_on_event returns; copy what
                          you keep. */
    size_t text_len;
    int code;          /* RESULT: storage.get: 1 found, 0 missing; http.request: the HTTP status */
    int truncated;     /* RESULT of http.request: 1 when the body was cut to fit */
    const char *location; /* RESULT of http.request: the redirect target of a 3xx, else "" */
    /* Since SDK 0.8. */
    aitbase_key_t key;         /* KEY: which button */
    aitbase_gesture_t gesture; /* KEY: press or long press */
} aitbase_event_t;

typedef enum aitbase_log_level {
    AITBASE_LOG_ERROR = 0,
    AITBASE_LOG_WARN = 1,
    AITBASE_LOG_INFO = 2,
    AITBASE_LOG_DEBUG = 3,
} aitbase_log_level_t;

typedef struct aitbase_plugin aitbase_plugin_t;

/* Plugin identity, matching "id" in plugin.json. Use once at file scope. */
#define AITBASE_PLUGIN_ID(id) const char aitbase_plugin_id[] = id
extern const char aitbase_plugin_id[];

/* --- Callbacks the plugin implements ---------------------------------------------------------- */

/* Called once after the runtime connects. Open the screen here. Nonzero stops the plugin. */
int aitbase_on_start(aitbase_plugin_t *plugin);
/* Called for every event, one at a time. */
void aitbase_on_event(aitbase_plugin_t *plugin, const aitbase_event_t *event);
/* Optional: called once before the plugin exits, for any reason. */
void aitbase_on_stop(aitbase_plugin_t *plugin);

/* --- Screen ------------------------------------------------------------------------------------ */

/* Opens a screen compiled from the plugin's ui/screens/<screen>.xml.
 * wait_ms = 0 fails with REJECTED if the device is busy. A nonzero wait (up to
 * AITBASE_UI_MAX_WAIT_MS) lets the open wait for the voice assistant to finish; voice-started
 * plugins should use one. */
int aitbase_ui_open(aitbase_plugin_t *plugin, const char *screen, uint32_t wait_ms);
/* Sets a bound value. node is the widget's full name ("status.title" for <lv_label name="title">
 * inside the component instance "status"); key is its bind_text subject. */
int aitbase_ui_update(aitbase_plugin_t *plugin, const char *node, const char *key,
                      const char *type, const char *value);
/* aitbase_ui_update(plugin, node, key, "string", text). */
int aitbase_ui_set_text(aitbase_plugin_t *plugin, const char *node, const char *key,
                        const char *text);
int aitbase_ui_close(aitbase_plugin_t *plugin);

/* --- Storage ("storage"): small text values kept across runs, private to this plugin ----------- */

/* Keys: 1 to 64 letters, digits, '.', '_' or '-'. Values: up to 4096 bytes of UTF-8 text; tabs and
 * newlines are fine, other control characters are not. At most 128 keys and 64 KiB per plugin.
 * Needs an SD card in the device; without one requests fail with "no_storage". */

/* RESULT: code 1 and text = the value, or code 0 when the key is missing. */
int aitbase_storage_get(aitbase_plugin_t *plugin, const char *key);
/* Silent on success. */
int aitbase_storage_set(aitbase_plugin_t *plugin, const char *key, const char *value);
/* Silent on success, also when the key is missing. */
int aitbase_storage_delete(aitbase_plugin_t *plugin, const char *key);
/* RESULT: text = the keys, separated by newlines. */
int aitbase_storage_keys(aitbase_plugin_t *plugin);

/* --- HTTP ("http"): requests to public internet hosts ------------------------------------------- */

/* The device makes the request. http:// and https:// only; hosts that resolve to local,
 * private or reserved addresses are refused ("destination_blocked"). Redirects are not followed:
 * a 3xx RESULT has the target in event->location, and the plugin decides whether to request it.
 * One request at a time ("in_flight"); at most 1 a second with bursts of 5. The response body is
 * text, cut to fit the plugin's receive buffer (event->truncated). */
typedef struct aitbase_http_request {
    const char *method;          /* "GET", "POST", "PUT", "PATCH", "DELETE" or "HEAD"; NULL means GET */
    const char *url;             /* up to 1024 printable ASCII characters, no spaces */
    const char *const *headers;  /* "Name: value" strings, up to 16; NULL for none */
    size_t header_count;
    const char *body;            /* text, up to 8192 bytes; NULL for none */
    uint32_t timeout_ms;         /* 1000 to 15000; 0 means 10000 */
} aitbase_http_request_t;

/* RESULT: code = the HTTP status, text = the body. */
int aitbase_http_request(aitbase_plugin_t *plugin, const aitbase_http_request_t *request);
/* aitbase_http_request() with GET and no headers. */
int aitbase_http_get(aitbase_plugin_t *plugin, const char *url);

/* --- Sound ("audio.output"): only while the plugin's screen is visible --------------------------- */

/* A new sound replaces the one playing. Each ends with AITBASE_EVENT_SOUND_DONE. Sounds stop when
 * the device takes the screen back. volume is 0 to 100. */
int aitbase_audio_tone(aitbase_plugin_t *plugin, uint32_t freq_hz, uint32_t duration_ms, uint32_t volume);
/* Plays sounds/<sound>.wav from the package: 16 kHz, mono, 16-bit PCM, at most 10 s. List it in
 * plugin.json under "sounds". */
int aitbase_audio_play(aitbase_plugin_t *plugin, const char *sound, uint32_t volume);
int aitbase_audio_stop(aitbase_plugin_t *plugin);

/* --- Plugin ------------------------------------------------------------------------------------ */

/* --- Synchronous calls (since SDK 0.10) --------------------------------------------------------- */

/* The device's answer to aitbase_call(). Set result and result_cap to receive the result. */
typedef struct aitbase_reply {
    int status;         /* 0, or the device's status, AITBASE_DEVICE_* */
    char error[65];     /* the device's stable error code when status is not 0 */
    char *result;       /* your buffer for the result object, as JSON text; may be NULL */
    size_t result_cap;
    size_t result_len;  /* the result's length; larger than result_cap - 1 when it didn't fit */
} aitbase_reply_t;

/* Sends <cap>.<op> with args (a JSON object as text, or NULL for none) and waits up to timeout_ms
 * for the device's reply: for operations that have no function of their own, for example the
 * firmware feature module operations schedule.*, settings.* and status.publish.
 *
 * Returns AITBASE_OK when the device replied (check reply->status), AITBASE_ERR_TIMEOUT,
 * AITBASE_ERR_IO, AITBASE_ERR_ARGUMENT, or AITBASE_ERR_NO_MEMORY when the result didn't fit
 * (reply->result_len says how much it needed). Events that arrive while it waits are delivered
 * after the current callback returns, in order. Don't call it before aitbase_on_start.
 *
 * Native runtime only: in WASM it returns AITBASE_ERR_UNSUPPORTED. */
int aitbase_call(aitbase_plugin_t *plugin, const char *cap, const char *op, const char *args_json,
                 uint32_t timeout_ms, aitbase_reply_t *reply);

/* Delivers AITBASE_EVENT_TIMER every interval_ms (minimum 10). 0 stops the timer. */
int aitbase_timer_set(aitbase_plugin_t *plugin, uint32_t interval_ms);
/* Stops the plugin after the current callback returns; its screen is closed. */
void aitbase_plugin_stop(aitbase_plugin_t *plugin);
/* Why the device started the plugin, from aitbase_on_start on (since SDK 0.9):
 *   "autorun"  a new or changed package on the SD card, or `aitbase-plugin dev`;
 *   "voice"    the user asked the assistant to open it;
 *   "shortcut" the owner's button shortcut (RIGHT long press on the home screen);
 *   "schedule" the manifest's schedule ("triggers"): the user didn't ask, so do the work and stop,
 *              or open the screen only when there is something to show. A scheduled run without a
 *              screen is ended after 30 s;
 *   ""         an older device, which doesn't say. */
const char *aitbase_trigger(aitbase_plugin_t *plugin);
/* Writes a line to the plugin log on the device. */
void aitbase_log(aitbase_plugin_t *plugin, aitbase_log_level_t level, const char *fmt, ...)
#if defined(__GNUC__)
    __attribute__((format(printf, 3, 4)))
#endif
    ;

const char *aitbase_event_kind_name(aitbase_event_kind_t kind);

#ifdef __cplusplus
}
#endif

#endif /* AITBASE_PLUGIN_H */
