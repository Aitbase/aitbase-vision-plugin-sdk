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
 * Functions return 0 on success or a negative aitbase_status_t.
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
} aitbase_event_kind_t;

typedef struct aitbase_event {
    aitbase_event_kind_t kind;
    char node[33];
    char action[33];
    char op[16];
    int status;
    char detail[65];
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

/* --- Plugin ------------------------------------------------------------------------------------ */

/* Delivers AITBASE_EVENT_TIMER every interval_ms (minimum 10). 0 stops the timer. */
int aitbase_timer_set(aitbase_plugin_t *plugin, uint32_t interval_ms);
/* Stops the plugin after the current callback returns; its screen is closed. */
void aitbase_plugin_stop(aitbase_plugin_t *plugin);
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
