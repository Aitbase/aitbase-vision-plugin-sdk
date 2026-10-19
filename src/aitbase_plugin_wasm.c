/*
 * WASM runtime for aitbase_plugin.h: the guest half of the device's WASM host ABI
 * (docs/wasm-abi.md). The device's plugin runner owns the loop and the connection; it calls the
 * exports below one at a time, and this file turns them into the plugin's callbacks through the
 * shared protocol core (aitbase_plugin_core.c).
 *
 * Built for wasm32 (wasi-sdk, reactor model). The tests build it natively and provide the
 * imports as ordinary functions.
 */
#include "aitbase_plugin.h"
#include "aitbase_plugin_core.h"

#include <string.h>

#if defined(__wasm__)
#define AITBASE_IMPORT(name) __attribute__((import_module("aitbase"), import_name(name)))
#define AITBASE_EXPORT(name) __attribute__((export_name(name)))
#else
#define AITBASE_IMPORT(name)
#define AITBASE_EXPORT(name)
#endif

#define AITBASE_WASM_ABI_VERSION 1

/* Results of the exports that run plugin code. */
#define AITBASE_WASM_CONTINUE 0
#define AITBASE_WASM_STOPPED 1
#define AITBASE_WASM_FAILED (-1)

/* --- Imports: provided by the device's plugin runner ----------------------------------------- */

AITBASE_IMPORT("send") int32_t aitbase_host_send(const char *message, uint32_t len);
AITBASE_IMPORT("log") void aitbase_host_log(int32_t level, const char *text, uint32_t len);
AITBASE_IMPORT("timer_set") int32_t aitbase_host_timer_set(uint32_t interval_ms);
AITBASE_IMPORT("package_read")
int32_t aitbase_host_package_read(const char *name, uint32_t name_len, char *buf, uint32_t cap);
AITBASE_IMPORT("audio_write") int32_t aitbase_host_audio_write(const void *pcm, uint32_t len);

/* --- Transport -------------------------------------------------------------------------------- */

static aitbase_plugin_t g_plugin;

int aitbase_transport_send(aitbase_plugin_t *plugin, const char *message, size_t len) {
    (void)plugin;
    return aitbase_host_send(message, (uint32_t)len) == 0 ? AITBASE_OK : AITBASE_ERR_IO;
}

void aitbase_transport_log(aitbase_log_level_t level, const char *text) {
    aitbase_host_log((int32_t)level, text, (uint32_t)strlen(text));
}

/* The WASM host ABI has no blocking receive: synchronous calls are native only. */
int aitbase_transport_can_wait(void) { return 0; }

int aitbase_transport_take_reply(aitbase_plugin_t *plugin, uint32_t id, uint32_t timeout_ms, struct cJSON **reply) {
    (void)plugin;
    (void)id;
    (void)timeout_ms;
    (void)reply;
    return AITBASE_ERR_UNSUPPORTED;
}

int aitbase_transport_read_screen(aitbase_plugin_t *plugin, const char *screen, char *buf, size_t cap,
                                  size_t *len) {
    char name[48];
    const size_t n = strlen(screen);
    (void)plugin;
    memcpy(name, screen, n); /* the core checked the screen name: at most 32 characters */
    memcpy(name + n, ".ui.json", 8);
    const int32_t rc = aitbase_host_package_read(name, (uint32_t)(n + 8), buf, (uint32_t)cap);
    if (rc < 0) return rc == AITBASE_ERR_NOT_FOUND ? AITBASE_ERR_NOT_FOUND : AITBASE_ERR_ARGUMENT;
    *len = (size_t)rc;
    return AITBASE_OK;
}

/* The runner frames it and finishes a partly sent frame itself (docs/wasm-abi.md). */
int aitbase_transport_audio_write(aitbase_plugin_t *plugin, const void *pcm, size_t len) {
    (void)plugin;
    const int32_t rc = aitbase_host_audio_write(pcm, (uint32_t)len);
    if (rc >= 0) return (int)rc;
    if (rc == AITBASE_ERR_UNSUPPORTED || rc == AITBASE_ERR_ARGUMENT) return (int)rc;
    return AITBASE_ERR_IO;
}

int aitbase_timer_set(aitbase_plugin_t *plugin, uint32_t interval_ms) {
    if (!plugin) return AITBASE_ERR_ARGUMENT;
    if (interval_ms > 0 && interval_ms < AITBASE_MIN_TIMER_MS) interval_ms = AITBASE_MIN_TIMER_MS;
    return aitbase_host_timer_set(interval_ms) == 0 ? AITBASE_OK : AITBASE_ERR_ARGUMENT;
}

/* After plugin code ran: finish once the plugin asked to stop. */
static int32_t after_callback(void) {
    if (!g_plugin.stop) return AITBASE_WASM_CONTINUE;
    aitbase_core_finish(&g_plugin);
    return AITBASE_WASM_STOPPED;
}

/* --- Exports: called by the device's plugin runner ------------------------------------------- */

AITBASE_EXPORT("aitbase_abi_version") int32_t aitbase_wasm_abi_version(void) {
    return AITBASE_WASM_ABI_VERSION;
}

/* The runner copies each device message here, without its newline. */
AITBASE_EXPORT("aitbase_rx_buffer") char *aitbase_wasm_rx_buffer(void) {
    return g_plugin.rx;
}

/* The receive buffer's size. Optional for the runner: without it, it assumes 4096 bytes. */
AITBASE_EXPORT("aitbase_rx_capacity") int32_t aitbase_wasm_rx_capacity(void) {
    return (int32_t)sizeof(g_plugin.rx);
}

AITBASE_EXPORT("aitbase_start") int32_t aitbase_wasm_start(void) {
    return aitbase_core_send_hello(&g_plugin) == AITBASE_OK ? AITBASE_WASM_CONTINUE : AITBASE_WASM_FAILED;
}

AITBASE_EXPORT("aitbase_message") int32_t aitbase_wasm_message(uint32_t len) {
    if (g_plugin.finished) return AITBASE_WASM_STOPPED;
    if (len > sizeof(g_plugin.rx)) return AITBASE_WASM_FAILED;
    if (aitbase_core_handle_message(&g_plugin, g_plugin.rx, len) != AITBASE_OK) return AITBASE_WASM_FAILED;
    return after_callback();
}

AITBASE_EXPORT("aitbase_timer") int32_t aitbase_wasm_timer(void) {
    aitbase_event_t tick;
    if (g_plugin.finished) return AITBASE_WASM_STOPPED;
    if (!g_plugin.ready) return AITBASE_WASM_CONTINUE;
    memset(&tick, 0, sizeof(tick));
    tick.kind = AITBASE_EVENT_TIMER;
    aitbase_core_dispatch(&g_plugin, &tick);
    return after_callback();
}

/* The device is ending the plugin (shutdown, disconnect): run aitbase_on_stop once. */
AITBASE_EXPORT("aitbase_stop") void aitbase_wasm_stop(void) {
    aitbase_core_finish(&g_plugin);
}
