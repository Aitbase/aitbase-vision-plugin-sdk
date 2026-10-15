#ifndef AITBASE_PLUGIN_H
#define AITBASE_PLUGIN_H

/*
 * AITBASE VISION plugin API (C), version 0 — experimental, will change before 1.0.
 *
 * A plugin owns one screen on the device. The device owns rendering, focus and the exit gesture:
 * the user moves focus with LEFT, activates with RIGHT and leaves with a LEFT long press. The
 * device may take the screen back at any time (voice assistant, settings...), which the plugin
 * sees as AITBASE_EVENT_PREEMPTED.
 *
 * Plugins talk only to this API. In v0 it is implemented over the development bridge socket of a
 * developer-enabled device; the planned WASM runtime implements the same calls, so plugin code
 * written against this header does not depend on the transport.
 *
 * Calls are asynchronous: a request returns once it is sent, and its outcome arrives as an event
 * from aitbase_plugin_next_event(). Functions return 0 on success or a negative aitbase_status_t.
 */

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#define AITBASE_PLUGIN_API_VERSION 0

/* Longest wait accepted by aitbase_ui_open(); the device clamps larger values. */
#define AITBASE_UI_MAX_WAIT_MS 30000u

typedef enum aitbase_status {
    AITBASE_OK = 0,
    AITBASE_ERR_ARGUMENT = -1,   /* NULL or oversized argument */
    AITBASE_ERR_CONNECT = -2,    /* device endpoint unavailable */
    AITBASE_ERR_IO = -3,         /* connection lost */
    AITBASE_ERR_PROTOCOL = -4,   /* malformed message from the device */
    AITBASE_ERR_NO_MEMORY = -5,
    AITBASE_ERR_FILE = -6,       /* cannot read the compiled UI file */
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
} aitbase_event_kind_t;

typedef struct aitbase_event {
    aitbase_event_kind_t kind;
    char node[33];
    char action[33];
    char op[16];
    int status;
    char detail[65];
} aitbase_event_t;

typedef struct aitbase_plugin aitbase_plugin_t;

/* Connects to the device. endpoint is NULL for the default. Returns NULL on failure. */
aitbase_plugin_t *aitbase_plugin_connect(const char *endpoint);
/* Disconnects; the device closes this plugin's screen. Accepts NULL. */
void aitbase_plugin_disconnect(aitbase_plugin_t *plugin);

/* Opens the plugin screen from a compiled aitbase.plugin-ui JSON document.
 * wait_ms = 0 fails if the device is busy; a nonzero wait (max AITBASE_UI_MAX_WAIT_MS) lets the
 * open wait for the voice assistant to finish, which is what voice-started plugins should use. */
int aitbase_ui_open(aitbase_plugin_t *plugin, const char *ui_json, size_t len, uint32_t wait_ms);
/* Same as aitbase_ui_open() with the document read from a file (aitbase-plugin-ui compiler output). */
int aitbase_ui_open_file(aitbase_plugin_t *plugin, const char *path, uint32_t wait_ms);
/* Sets a bound value. node is the compiled node id (e.g. "status.title"), key the binding key. */
int aitbase_ui_update(aitbase_plugin_t *plugin, const char *node, const char *key,
                      const char *type, const char *value);
/* Convenience for string bindings: aitbase_ui_update(plugin, node, key, "string", text). */
int aitbase_ui_set_text(aitbase_plugin_t *plugin, const char *node, const char *key,
                        const char *text);
int aitbase_ui_close(aitbase_plugin_t *plugin);

/* Waits up to timeout_ms (-1 = forever) for the next event.
 * Returns 1 with *out filled, 0 on timeout, or a negative aitbase_status_t. */
int aitbase_plugin_next_event(aitbase_plugin_t *plugin, aitbase_event_t *out, int timeout_ms);

const char *aitbase_event_kind_name(aitbase_event_kind_t kind);

#ifdef __cplusplus
}
#endif

#endif /* AITBASE_PLUGIN_H */
