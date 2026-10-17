#ifndef AITBASE_PLUGIN_CORE_H
#define AITBASE_PLUGIN_CORE_H

/*
 * SDK-internal: the protocol core shared by both runtimes. It builds requests, parses device
 * messages and calls the plugin's callbacks (docs/plugin-protocol.md). A transport moves the bytes:
 * aitbase_plugin_dev.c over a socket for native builds, aitbase_plugin_wasm.c through the device's
 * WASM host ABI (docs/wasm-abi.md).
 */

#include <stddef.h>
#include <stdint.h>

#include "aitbase_plugin.h"

#define AITBASE_PROTOCOL_VERSION 1
#define AITBASE_MAX_UI_BYTES 16384u
/* Receive buffer: the longest device message the plugin accepts. Sent as rx_max in hello; the
 * device keeps replies within it (AITBASE_LIMIT_RX_MAX at most). */
#define AITBASE_RX_BUFFER 16384u
#define AITBASE_PENDING 32u
#define AITBASE_MIN_TIMER_MS 10u

struct aitbase_plugin {
    int ready;
    int stop;
    int finished;
    uint32_t next_id;
    struct {
        uint32_t id;
        char op[16];
        int tracked; /* the plugin holds the id: RESULT, SOUND_DONE and ERROR carry it */
    } pending[AITBASE_PENDING];
    /* Native transport only. */
    int fd;
    uint32_t timer_ms;
    long long timer_due;
    size_t used;
    char rx[AITBASE_RX_BUFFER];
    char ui_dir[256];
};

/* --- Implemented by the transport ------------------------------------------------------------ */

/* Sends one message; the transport adds the framing. */
int aitbase_transport_send(aitbase_plugin_t *plugin, const char *message, size_t len);
/* Writes one formatted log line. */
void aitbase_transport_log(aitbase_log_level_t level, const char *text);
/* Copies the compiled screen <screen>.ui.json into buf. Sets *len; returns a status. */
int aitbase_transport_read_screen(aitbase_plugin_t *plugin, const char *screen, char *buf, size_t cap,
                                  size_t *len);

/* --- Provided by the core -------------------------------------------------------------------- */

/* Sends system.hello. */
int aitbase_core_send_hello(aitbase_plugin_t *plugin);
/* Handles one device message (without its newline): marks the handshake, calls aitbase_on_start
 * after it and delivers events. Returns 0 or a negative status; check plugin->stop afterwards. */
int aitbase_core_handle_message(aitbase_plugin_t *plugin, const char *line, size_t len);
/* Delivers one event to the plugin. */
void aitbase_core_dispatch(aitbase_plugin_t *plugin, const aitbase_event_t *event);
/* Once: closes the screen if the handshake completed, then calls aitbase_on_stop. */
void aitbase_core_finish(aitbase_plugin_t *plugin);

#endif /* AITBASE_PLUGIN_CORE_H */
