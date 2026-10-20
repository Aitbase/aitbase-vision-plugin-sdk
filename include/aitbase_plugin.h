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
 * Every plugin may use every capability: there is nothing to request in plugin.json (since SDK
 * 0.11; devices before 2026-10-20 still need requested_capabilities). What the device enforces is
 * in each call's rules, for example sound only while the plugin's screen shows. The device's
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
    AITBASE_EVENT_STREAM_STATE = 13,   /* the audio stream changed: detail is "paused", "resumed" or
                                          "stopped", and reason says why it stopped. Since SDK 0.10 */
    AITBASE_EVENT_TOOL_CALL = 14,      /* the assistant calls one of the plugin's assistant_tools:
                                          action is the tool, request the call id for
                                          aitbase_tool_reply(), text the arguments as a JSON object.
                                          Since SDK 0.10 */
    AITBASE_EVENT_CHANGED = 15,        /* the user changed an input widget: node is the widget, code its
                                          value (0 or 1 for a checkbox or switch, a slider's value, a
                                          roller's or dropdown's option index). At most 10 a second;
                                          the latest value wins. Needs firmware with D36 input widgets.
                                          Since SDK 0.12 */
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
    char op[24];       /* ERROR, RESULT: the request's operation: "open", "update", "close",
                          "storage.get", "http.request", "audio.play"... (24 bytes since SDK 0.10) */
    int status;        /* ERROR: a device status, AITBASE_DEVICE_* */
    char detail[65];   /* ERROR: a stable error code; SOUND_DONE: how the sound ended */
    /* Since SDK 0.5. */
    uint32_t request;  /* ERROR, RESULT, SOUND_DONE: the id the request function returned; 0 for
                          screen requests. TOOL_CALL: the call id */
    const char *text;  /* RESULT: storage.get: the value; storage.keys: the keys, one per line;
                          http.request: the response body, text only. DEVICE: the event's data
                          as JSON. TOOL_CALL: the arguments as JSON. "" otherwise. Valid until aitbase_on_event returns; copy what
                          you keep. */
    size_t text_len;
    int code;          /* RESULT: storage.get: 1 found, 0 missing; http.request: the HTTP status.
                          CHANGED: the input widget's value */
    int truncated;     /* RESULT of http.request: 1 when the body was cut to fit */
    const char *location; /* RESULT of http.request: the redirect target of a 3xx, else "" */
    /* Since SDK 0.8. */
    aitbase_key_t key;         /* KEY: which button */
    aitbase_gesture_t gesture; /* KEY: press or long press */
    /* Since SDK 0.10. */
    char reason[17];   /* STREAM_STATE "stopped": "closed", "preempted", "replaced", "idle_timeout",
                          "bad_stream", "user" or "failed"; "" otherwise */
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

/* One of the device's own screen layouts, filled with your text (since SDK 0.10; device decision
 * D27). Use it instead of a compiled screen when the layout fits:
 *   "status": title, optional emoji line, an OK button: RIGHT gives the action "template.ok";
 *   "alert":  an icon ("bell", "info" or "warning"; NULL for bell), title, an OK button;
 *   "menu":   title and 1 to 4 items, without gaps; LEFT moves between them, RIGHT on item N gives
 *             the action "template.itemN".
 * Text: one line each, no control or invisible characters; title at most 48 bytes, emoji 8,
 * items 24. Like aitbase_ui_open it replaces your screen and waits up to wait_ms if the device is
 * busy; OPENED, REJECTED and PREEMPTED follow as for any screen. Call it again to change the text. */
typedef struct aitbase_template {
    const char *kind;      /* "status", "alert" or "menu" */
    const char *title;
    const char *emoji;     /* status only; NULL for none */
    const char *icon;      /* alert only; NULL for "bell" */
    const char *items[4];  /* menu only; NULL ends the list */
} aitbase_template_t;
int aitbase_ui_template(aitbase_plugin_t *plugin, const aitbase_template_t *tpl, uint32_t wait_ms);

/* --- Storage ("storage"): small text values kept across runs, private to this plugin ----------- */

/* Keys: 1 to 64 letters, digits, '.', '_' or '-'. Values: up to 4096 bytes of UTF-8 text; tabs and
 * newlines are fine, other control characters are not. At most 128 keys and 64 KiB per plugin.
 * Needs an SD card in the device; without one requests fail with "no_storage". */

/* Settings (since SDK 0.12, device decision D38): the values the owner fills in on the Device
 * Console for the fields in plugin.json's "settings" are in storage as "settings.<key>", for
 * example aitbase_storage_get(p, "settings.ha_token"). A running plugin gets AITBASE_EVENT_DEVICE
 * with op "plugin.settings_changed" when the owner saves; read them again then. */

/* RESULT: code 1 and text = the value, or code 0 when the key is missing. */
int aitbase_storage_get(aitbase_plugin_t *plugin, const char *key);
/* Silent on success. */
int aitbase_storage_set(aitbase_plugin_t *plugin, const char *key, const char *value);
/* Silent on success, also when the key is missing. */
int aitbase_storage_delete(aitbase_plugin_t *plugin, const char *key);
/* RESULT: text = the keys, separated by newlines. */
int aitbase_storage_keys(aitbase_plugin_t *plugin);

/* --- Alarms ("schedule"): the device rings at set times (since SDK 0.10) ------------------------ */

/* Entries the device keeps and rings itself, on its own alarm screen titled "<plugin name>:
 * <label>", with its built-in alarm sound, whether or not the plugin runs (device decisions D23,
 * D26). Any plugin may set them (since SDK 0.11; older devices asked the owner to "Allow alarms"
 * first). At most 4 entries, at least 5 minutes apart ("too_many", "too_close"); a ring stops after
 * 60 s, and the next alarm shows on the device's home screen. Ids: 1 to
 * 14 of a-z, 0-9 and '_'. Labels: one line of at most 48 bytes, no control or invisible
 * characters. due_epoch: Unix seconds, at most 366 days ahead.
 *
 * When a ringing entry is due, the device rings it at once, once; an entry whose time passed while
 * the device was off never rings. If the plugin runs then, it receives AITBASE_EVENT_DEVICE with op
 * "schedule.due" and text {"id":"<id>"} afterwards. A silent entry (rings = 0) only delivers
 * schedule.due, and only if the plugin runs then. */

/* Adds or moves the entry. Silent on success. */
int aitbase_schedule_set(aitbase_plugin_t *plugin, const char *id, int64_t due_epoch, int rings, const char *label);
/* RESULT: code 1 when the entry existed, 0 when not. */
int aitbase_schedule_cancel(aitbase_plugin_t *plugin, const char *id);
/* RESULT: text = the plugin's entries as JSON: [{"id","due_epoch","rings","label"}, ...]. */
int aitbase_schedule_list(aitbase_plugin_t *plugin);
/* Does nothing: the device rings due entries itself (since SDK 0.11). Kept so 0.10 code builds. */
int aitbase_schedule_ring(aitbase_plugin_t *plugin, const char *id);

/* --- Idle face ("idle.face"): the plugin's face for the home screen (since SDK 0.10) ----------- */

/* A plugin may offer one of its screens as the device's home screen face, in place of the clock:
 * "idle_face": "<screen>" in plugin.json, naming one of ui.screens (device decision D22). The owner
 * chooses it on the Device Console; the device then shows it display only, under its own "Plugin"
 * badge, without running the plugin. A face has no buttons or actions, at most 24 widgets and
 * 8 KiB compiled; `aitbase-plugin check` tells. Its labels bind to "face.1" to "face.16"
 * (<lv_label bind_text="face.1"/>). */

/* Sets the text of the face's labels bound to key ("face.1" to "face.16"): one line of at most 64
 * bytes, no control or invisible characters. The device keeps the value (on the SD card), so the
 * face shows it after a reboot, and keeps it whether or not the owner has chosen the face yet.
 * At most 1 a second with bursts of 5. Silent on success. */
int aitbase_face_set(aitbase_plugin_t *plugin, const char *key, const char *value);

/* --- HTTP ("http"): requests to internet and local network hosts --------------------------------- */

/* The device makes the request. http:// and https:// only. Internet hosts and, on devices with D37
 * (since SDK 0.12), the local network (10/8, 172.16/12, 192.168/16, fc00::/7) are allowed, for
 * example a Home Assistant at http://192.168.1.10:8123. The device's own addresses, loopback,
 * link-local and reserved addresses are refused ("destination_blocked"). Redirects are not followed:
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

/* --- Audio stream ("audio.stream", D28): continuous sound while the plugin's screen is visible -- */

/* Samples one aitbase_audio_stream_write() takes at most: AITBASE_LIMIT_AUDIO_STREAM_FRAME_MAX
 * bytes of 16-bit samples. */
#define AITBASE_AUDIO_STREAM_FRAME_SAMPLES (AITBASE_LIMIT_AUDIO_STREAM_FRAME_MAX / 2u)

/* Opens the plugin's stream of 16-bit mono PCM at rate Hz: 8000, 11025, 12000, 16000, 22050,
 * 24000, 32000, 44100 or 48000. Stops any sound playing; sounds are refused ("streaming") until
 * the stream closes. Returns a request id. A refusal arrives as AITBASE_EVENT_ERROR with that id:
 * "speaker_busy" (an alarm, a call or the voice assistant holds the speaker, or another plugin
 * streams), "not_foreground", "bad_rate" or "no_channel". Afterwards the stream reports
 * AITBASE_EVENT_STREAM_STATE: "paused" while the device's own audio plays (keep your samples;
 * writes return 0 then), "resumed", and "stopped" with a reason. The stream stops when the device
 * takes the screen back, and after 10 s without samples. At most 5 opens and closes a second. */
int aitbase_audio_stream_open(aitbase_plugin_t *plugin, uint32_t rate);
/* Sends up to AITBASE_AUDIO_STREAM_FRAME_SAMPLES samples. Never blocks: returns samples when the
 * device took them, 0 while its buffer is full (send the same samples again later, for example on
 * the next timer), or a negative status: AITBASE_ERR_ARGUMENT for none or too many,
 * AITBASE_ERR_IO once the audio channel is closed (after a "bad_stream", it stays closed for the
 * run), AITBASE_ERR_UNSUPPORTED when plugin.json doesn't request "audio.stream". Samples sent
 * while no stream is open end the channel ("bad_stream"). */
int aitbase_audio_stream_write(aitbase_plugin_t *plugin, const int16_t *pcm, size_t samples);
/* Plays out the samples already sent, then closes the stream: AITBASE_EVENT_STREAM_STATE
 * "stopped" with reason "closed". Returns a request id. */
int aitbase_audio_stream_close(aitbase_plugin_t *plugin);

/* --- Background audio (D29). Feature modules only: firmware packages that request
 * audio.background, native, with the device's module host. Since SDK 0.10. */

/* Opens the stream as a background stream, which keeps playing after the screen closes. The device
 * shows it on its own screen, titled "<plugin name>: <title>" (title NULL or "" for the name alone;
 * at most 48 characters), below the assistant and any plugin screen. Refusals are AITBASE_EVENT_ERROR
 * with the request id: "module_only" (not a feature module, or no audio.background), "speaker_busy",
 * "bad_rate" or "no_channel". Otherwise as aitbase_audio_stream_open(). Returns a request id. */
int aitbase_audio_stream_open_background(aitbase_plugin_t *plugin, uint32_t rate, const char *title);

/* --- Assistant tools ("assistant.tools", D31). Since SDK 0.10 ---------------------------------- */

/* A plugin answers the device's voice assistant through the tools it declares in plugin.json:
 *
 *   "assistant_tools": [{"name": "today", "description": "Today's weather for a city",
 *                        "args": {"city": {"type": "string", "max": 40}}}]
 *
 * At most 4 tools, each with at most 4 arguments: a string (one line, at most max bytes, up to
 * 120), an int with min and max, or an enum of up to 8 values. Every argument is required, and the
 * device checks each call against the declaration before the plugin sees it.
 *
 * When the assistant calls a tool, the plugin gets AITBASE_EVENT_TOOL_CALL: event->action is the
 * tool, event->request the call id and event->text the arguments. It answers once with
 * aitbase_tool_reply() within 5 s. A plugin that isn't running is started for the call without a
 * screen (aitbase_trigger() returns "assistant") and ends 2 s after its last call, unless it opens
 * a screen; the screen then waits for the conversation to end (pass wait_ms to aitbase_ui_open).
 * The assistant reads the reply as the plugin's own words, never as instructions; control,
 * bidirectional and zero-width characters are removed. */

/* Answers the tool call with this id: text is one reply of at most 2048 bytes of UTF-8. Returns a
 * request id. Refusals are AITBASE_EVENT_ERROR with it: "tool_unknown" (no call with this id waits
 * for a reply: it timed out, or was answered already) or "bad_request" (too long). */
int aitbase_tool_reply(aitbase_plugin_t *plugin, uint32_t call, const char *text);
/* Reads a string or enum argument of an AITBASE_EVENT_TOOL_CALL into out (cap bytes, NUL
 * terminated). Returns its length, AITBASE_ERR_ARGUMENT when the event has no such string
 * argument, or AITBASE_ERR_NO_MEMORY when it doesn't fit. Valid only while that event is handled. */
int aitbase_tool_arg_string(const aitbase_event_t *event, const char *name, char *out, size_t cap);
/* Reads an int argument of an AITBASE_EVENT_TOOL_CALL. AITBASE_OK, or AITBASE_ERR_ARGUMENT when the
 * event has no such int argument. */
int aitbase_tool_arg_int(const aitbase_event_t *event, const char *name, long *out);

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
 *   "assistant" the assistant called one of the plugin's assistant_tools (since SDK 0.10): answer
 *              AITBASE_EVENT_TOOL_CALL; the run ends 2 s after the last call unless a screen opens;
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
