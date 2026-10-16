/*
 * Runs a real plugin through the WASM runtime (src/aitbase_plugin_wasm.c), built natively: the
 * test plays the device's plugin runner. It provides the host imports of docs/wasm-abi.md as plain
 * functions and calls the exports the way the runner does.
 */
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "aitbase_plugin.h"
#include "cJSON.h"

/* --- The plugin under test ------------------------------------------------------------------- */

AITBASE_PLUGIN_ID("test.plugin");

static int g_ticks = 0;
static int g_stops = 0;

static void say(aitbase_plugin_t *p, const char *text) {
    aitbase_ui_set_text(p, "status.title", "title", text);
}

int aitbase_on_start(aitbase_plugin_t *p) {
    return aitbase_ui_open(p, "demo", 99999) == AITBASE_OK ? 0 : 1;
}

void aitbase_on_event(aitbase_plugin_t *p, const aitbase_event_t *ev) {
    char text[96];
    switch (ev->kind) {
        case AITBASE_EVENT_OPENED:
            say(p, "opened");
            aitbase_timer_set(p, 5);
            break;
        case AITBASE_EVENT_TIMER:
            snprintf(text, sizeof(text), "tick %d", ++g_ticks);
            say(p, text);
            if (g_ticks == 2) aitbase_timer_set(p, 0);
            break;
        case AITBASE_EVENT_ACTION:
            snprintf(text, sizeof(text), "action %s %s", ev->node, ev->action);
            say(p, text);
            break;
        case AITBASE_EVENT_ERROR:
            snprintf(text, sizeof(text), "error %s %d %s", ev->op, ev->status, ev->detail);
            say(p, text);
            break;
        case AITBASE_EVENT_PREEMPTED:
            aitbase_plugin_stop(p);
            break;
        default:
            break;
    }
}

void aitbase_on_stop(aitbase_plugin_t *p) {
    ++g_stops;
    aitbase_log(p, AITBASE_LOG_INFO, "stopping after %d ticks", g_ticks);
}

/* --- Exports of the runtime under test (docs/wasm-abi.md) ------------------------------------ */

int32_t aitbase_wasm_abi_version(void);
char *aitbase_wasm_rx_buffer(void);
int32_t aitbase_wasm_start(void);
int32_t aitbase_wasm_message(uint32_t len);
int32_t aitbase_wasm_timer(void);
void aitbase_wasm_stop(void);

/* --- The fake runner: host imports ----------------------------------------------------------- */

static int g_failed = 0;
#define EXPECT(cond, msg)                                                    \
    do {                                                                     \
        if (!(cond)) {                                                       \
            fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, msg);    \
            ++g_failed;                                                      \
        }                                                                    \
    } while (0)

static char g_sent[16][20480];
static int g_sent_count = 0;
static int g_read_count = 0;
static uint32_t g_timer_ms = 0;
static char g_last_log[256];

static const char kScreen[] = "{\"format\":\"aitbase.plugin-ui\",\"version\":1,\"screen\":\"demo\"}\n";

int32_t aitbase_host_send(const char *message, uint32_t len) {
    if (len == 0 || len >= sizeof(g_sent[0]) || memchr(message, '\n', len)) return -1;
    if (g_sent_count < 16) {
        memcpy(g_sent[g_sent_count], message, len);
        g_sent[g_sent_count][len] = '\0';
    }
    ++g_sent_count;
    return 0;
}

void aitbase_host_log(int32_t level, const char *text, uint32_t len) {
    snprintf(g_last_log, sizeof(g_last_log), "%d %.*s", (int)level, (int)len, text);
}

int32_t aitbase_host_timer_set(uint32_t interval_ms) {
    g_timer_ms = interval_ms;
    return 0;
}

int32_t aitbase_host_package_read(const char *name, uint32_t name_len, char *buf, uint32_t cap) {
    ++g_read_count;
    if (name_len != strlen("demo.ui.json") || memcmp(name, "demo.ui.json", name_len) != 0) return -6;
    if (sizeof(kScreen) - 1 > cap) return -1;
    memcpy(buf, kScreen, sizeof(kScreen) - 1);
    return (int32_t)(sizeof(kScreen) - 1);
}

/* --- Driving the exports --------------------------------------------------------------------- */

static int32_t deliver(const char *line) {
    const size_t len = strlen(line);
    memcpy(aitbase_wasm_rx_buffer(), line, len);
    return aitbase_wasm_message((uint32_t)len);
}

/* Takes the next sent message, parsed. */
static cJSON *take(int *next) {
    if (*next >= g_sent_count) return NULL;
    return cJSON_Parse(g_sent[(*next)++]);
}

static const char *str(const cJSON *obj, const char *name) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(obj, name);
    return cJSON_IsString(item) ? item->valuestring : "";
}

static int num(const cJSON *obj, const char *name) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(obj, name);
    return cJSON_IsNumber(item) ? item->valueint : -9999;
}

static void expect_update(int *next, const char *value) {
    cJSON *req = take(next);
    const cJSON *args = cJSON_GetObjectItem(req, "args");
    const int ok = req && strcmp(str(req, "op"), "update") == 0 && strcmp(str(args, "value"), value) == 0;
    if (!ok) fprintf(stderr, "expected update \"%s\", got %s\n", value, req ? str(args, "value") : "nothing");
    EXPECT(ok, "update request");
    cJSON_Delete(req);
}

int main(void) {
    int next = 0;
    char line[256];
    cJSON *req;

    EXPECT(aitbase_wasm_abi_version() == 1, "ABI version 1");
    EXPECT(aitbase_wasm_rx_buffer() != NULL, "receive buffer");

    /* start sends the handshake, and nothing else until the device accepts it. */
    EXPECT(aitbase_wasm_start() == 0, "start continues");
    req = take(&next);
    EXPECT(req && strcmp(str(req, "op"), "hello") == 0, "hello first");
    EXPECT(strcmp(str(cJSON_GetObjectItem(req, "args"), "plugin"), "test.plugin") == 0, "hello carries the plugin id");
    EXPECT(g_sent_count == 1, "nothing before the handshake reply");
    snprintf(line, sizeof(line), "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{\"protocol\":1}}", num(req, "id"));
    cJSON_Delete(req);

    /* The reply runs aitbase_on_start, which opens the screen read through package_read. */
    EXPECT(deliver(line) == 0, "hello reply continues");
    req = take(&next);
    EXPECT(req && strcmp(str(req, "op"), "open") == 0, "open after the handshake");
    EXPECT(strcmp(str(cJSON_GetObjectItem(cJSON_GetObjectItem(req, "args"), "document"), "format"), "aitbase.plugin-ui") == 0,
           "document embedded from package_read");
    EXPECT(num(cJSON_GetObjectItem(req, "args"), "wait_ms") == (int)AITBASE_UI_MAX_WAIT_MS, "wait is clamped");
    EXPECT(g_read_count == 1, "one package read");
    cJSON_Delete(req);

    EXPECT(deliver("{\"cap\":\"ui\",\"event\":\"from_a_newer_device\",\"data\":{}}") == 0, "unknown events are ignored");
    EXPECT(deliver("{\"cap\":\"ui\",\"event\":\"opened\",\"data\":{\"node\":\"\",\"action\":\"\",\"detail\":\"\"}}") == 0, "opened");
    expect_update(&next, "opened");
    EXPECT(g_timer_ms == 10, "timer_set clamps to 10 ms and reaches the host");

    EXPECT(aitbase_wasm_timer() == 0, "timer continues");
    expect_update(&next, "tick 1");
    EXPECT(aitbase_wasm_timer() == 0, "timer continues");
    expect_update(&next, "tick 2");
    EXPECT(g_timer_ms == 0, "timer_set(0) reaches the host");

    EXPECT(deliver("{\"cap\":\"ui\",\"event\":\"action\",\"data\":{\"node\":\"increment\",\"action\":\"plugin.increment\",\"detail\":\"\"}}") == 0, "action");
    expect_update(&next, "action increment plugin.increment");

    EXPECT(deliver("{\"id\":4,\"status\":-1,\"error\":\"bad_request\",\"result\":{}}") == 0, "error reply");
    expect_update(&next, "error update -1 bad_request");

    EXPECT(deliver("not json") == -1, "a malformed device message is a failure");
    EXPECT(aitbase_wasm_message(5000) == -1, "a length past the receive buffer is a failure");

    /* Preemption: the plugin stops; the runtime closes the screen and runs aitbase_on_stop once. */
    EXPECT(deliver("{\"cap\":\"ui\",\"event\":\"preempted\",\"data\":{\"node\":\"\",\"action\":\"\",\"detail\":\"\"}}") == 1, "stopped");
    req = take(&next);
    EXPECT(req && strcmp(str(req, "op"), "close") == 0, "close on stop");
    cJSON_Delete(req);
    EXPECT(g_stops == 1, "aitbase_on_stop ran");
    EXPECT(strcmp(g_last_log, "2 stopping after 2 ticks") == 0, "aitbase_log reaches the host log");
    aitbase_wasm_stop();
    EXPECT(g_stops == 1, "aitbase_stop after the plugin stopped does nothing");
    EXPECT(deliver("{\"cap\":\"ui\",\"event\":\"opened\",\"data\":{}}") == 1, "no events after the end");

    if (g_failed) {
        fprintf(stderr, "aitbase_plugin_wasm_test: %d failure(s)\n", g_failed);
        return 1;
    }
    fprintf(stderr, "aitbase_plugin_wasm_test: PASS\n");
    return 0;
}
