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
static int g_ids[4];
static uint32_t g_error_request = 99;

static void say(aitbase_plugin_t *p, const char *text) {
    aitbase_ui_set_text(p, "status.title", "title", text);
}

static int g_granted;
static char g_trigger[16];
static int g_call_rc = 1;
static aitbase_plugin_t *g_p;

int aitbase_on_start(aitbase_plugin_t *p) {
    g_p = p;
    snprintf(g_trigger, sizeof(g_trigger), "%s", aitbase_trigger(p));
    g_granted = aitbase_granted(p, "net") * 1 + aitbase_granted(p, "ui") * 2 + aitbase_granted(p, "http") * 4 +
                aitbase_granted(p, "audio.stream") * 8;
    aitbase_reply_t reply;
    memset(&reply, 0, sizeof(reply));
    g_call_rc = aitbase_call(p, "settings", "get", NULL, 10, &reply);
    return aitbase_ui_open(p, "demo", 99999) == AITBASE_OK ? 0 : 1;
}

void aitbase_on_event(aitbase_plugin_t *p, const aitbase_event_t *ev) {
    char text[160];
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
            if (strcmp(ev->node, "fetch") == 0) {
                g_ids[0] = aitbase_storage_get(p, "k");
                g_ids[1] = aitbase_http_get(p, "https://example.com/");
                g_ids[2] = aitbase_audio_tone(p, 440, 100, 150);
                g_ids[3] = aitbase_storage_keys(p);
                break;
            }
            snprintf(text, sizeof(text), "action %s %s", ev->node, ev->action);
            say(p, text);
            break;
        case AITBASE_EVENT_KEY:
            snprintf(text, sizeof(text), "key %s %s", ev->key == AITBASE_KEY_LEFT ? "left" : "right",
                     ev->gesture == AITBASE_GESTURE_LONG_PRESS ? "long" : "press");
            say(p, text);
            break;
        case AITBASE_EVENT_ERROR:
            g_error_request = ev->request;
            snprintf(text, sizeof(text), "error %s %d %s", ev->op, ev->status, ev->detail);
            say(p, text);
            break;
        case AITBASE_EVENT_RESULT:
            snprintf(text, sizeof(text), "result %s %u %d %s|%zu", ev->op, (unsigned)ev->request, ev->code, ev->text,
                     ev->text_len);
            say(p, text);
            break;
        case AITBASE_EVENT_SOUND_DONE:
            snprintf(text, sizeof(text), "sound %u %s", (unsigned)ev->request, ev->detail);
            say(p, text);
            break;
        case AITBASE_EVENT_STREAM_STATE:
            snprintf(text, sizeof(text), "stream %s %s", ev->detail, ev->reason);
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
int32_t aitbase_wasm_rx_capacity(void);
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

static char g_sent[32][20480];
static int g_sent_count = 0;
static int g_read_count = 0;
static uint32_t g_timer_ms = 0;
static char g_last_log[256];

static const char kScreen[] = "{\"format\":\"aitbase.plugin-ui\",\"version\":1,\"screen\":\"demo\"}\n";

int32_t aitbase_host_send(const char *message, uint32_t len) {
    if (len == 0 || len >= sizeof(g_sent[0]) || memchr(message, '\n', len)) return -1;
    if (g_sent_count < 32) {
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

/* The audio channel (D28): the last frame and what the next call returns. */
static unsigned char g_audio[8192];
static uint32_t g_audio_len;
static int32_t g_audio_result = 0; /* 0: take the frame */

int32_t aitbase_host_audio_write(const void *pcm, uint32_t len) {
    if (g_audio_result != 0) return g_audio_result;
    memcpy(g_audio, pcm, len);
    g_audio_len = len;
    return (int32_t)len;
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
    char text_expected[96];
    cJSON *req;

    EXPECT(aitbase_wasm_abi_version() == 1, "ABI version 1");
    EXPECT(aitbase_wasm_rx_buffer() != NULL, "receive buffer");
    EXPECT(aitbase_wasm_rx_capacity() == AITBASE_LIMIT_RX_MAX, "the receive buffer is the device's largest");

    /* start sends the handshake, and nothing else until the device accepts it. */
    EXPECT(aitbase_wasm_start() == 0, "start continues");
    req = take(&next);
    EXPECT(req && strcmp(str(req, "op"), "hello") == 0, "hello first");
    EXPECT(strcmp(str(cJSON_GetObjectItem(req, "args"), "plugin"), "test.plugin") == 0, "hello carries the plugin id");
    EXPECT(num(cJSON_GetObjectItem(req, "args"), "rx_max") == (int)AITBASE_LIMIT_RX_MAX, "hello declares the receive buffer");
    EXPECT(g_sent_count == 1, "nothing before the handshake reply");
    EXPECT(g_call_rc == 1, "aitbase_call is not tried before the handshake");
    snprintf(line, sizeof(line), "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{\"protocol\":1,\"trigger\":\"schedule\",\"grants\":\"ui, http.stream, net\"}}", num(req, "id"));
    cJSON_Delete(req);

    /* The reply runs aitbase_on_start, which opens the screen read through package_read. */
    EXPECT(deliver(line) == 0, "hello reply continues");
    EXPECT(strcmp(g_trigger, "schedule") == 0, "aitbase_trigger() tells on_start why the device started the plugin");
    EXPECT(g_granted == (1 | 2), "aitbase_granted() matches whole names from hello: net and ui, not http or audio.stream");
    EXPECT(g_call_rc == AITBASE_ERR_UNSUPPORTED, "aitbase_call is native only: WASM gets AITBASE_ERR_UNSUPPORTED and sends nothing");
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

    EXPECT(deliver("{\"cap\":\"ui\",\"event\":\"key\",\"data\":{\"key\":\"left\",\"gesture\":\"press\"}}") == 0, "key");
    expect_update(&next, "key left press");
    EXPECT(deliver("{\"cap\":\"ui\",\"event\":\"key\",\"data\":{\"key\":\"right\",\"gesture\":\"long_press\"}}") == 0, "long key");
    expect_update(&next, "key right long");
    EXPECT(deliver("{\"cap\":\"ui\",\"event\":\"key\",\"data\":{\"key\":\"up\",\"gesture\":\"press\"}}") == 0 &&
           deliver("{\"cap\":\"ui\",\"event\":\"key\",\"data\":{\"key\":\"left\",\"gesture\":\"double\"}}") == 0,
           "unknown keys and gestures are ignored");

    EXPECT(deliver("{\"id\":4,\"status\":-1,\"error\":\"bad_request\",\"result\":{}}") == 0, "error reply");
    expect_update(&next, "error update -1 bad_request");

    EXPECT(g_error_request == 0, "screen requests carry no request id");

    /* Broker requests return ids; their results and sound events carry them back. */
    EXPECT(deliver("{\"cap\":\"ui\",\"event\":\"action\",\"data\":{\"node\":\"fetch\",\"action\":\"plugin.fetch\"}}") == 0, "fetch");
    EXPECT(g_ids[0] > 0 && g_ids[1] > g_ids[0] && g_ids[2] > g_ids[1] && g_ids[3] > g_ids[2], "request ids");
    req = take(&next);
    EXPECT(req && strcmp(str(req, "cap"), "storage") == 0 && strcmp(str(req, "op"), "get") == 0 &&
           strcmp(str(cJSON_GetObjectItem(req, "args"), "key"), "k") == 0, "storage.get request");
    cJSON_Delete(req);
    req = take(&next);
    EXPECT(req && strcmp(str(req, "cap"), "http") == 0 && strcmp(str(cJSON_GetObjectItem(req, "args"), "method"), "GET") == 0 &&
           strcmp(str(cJSON_GetObjectItem(req, "args"), "url"), "https://example.com/") == 0, "http.request");
    cJSON_Delete(req);
    req = take(&next);
    EXPECT(req && strcmp(str(req, "op"), "tone") == 0 && num(cJSON_GetObjectItem(req, "args"), "volume") == 100,
           "audio.tone with the volume clamped");
    cJSON_Delete(req);
    req = take(&next);
    EXPECT(req && strcmp(str(req, "op"), "keys") == 0, "storage.keys");
    cJSON_Delete(req);

    snprintf(line, sizeof(line), "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{\"found\":true,\"value\":\"v1\"}}", g_ids[0]);
    EXPECT(deliver(line) == 0, "storage.get result");
    snprintf(text_expected, sizeof(text_expected), "result storage.get %d 1 v1|2", g_ids[0]);
    expect_update(&next, text_expected);
    snprintf(line, sizeof(line), "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{\"status\":302,\"body\":\"hi\",\"location\":\"https://b/\",\"truncated\":false}}", g_ids[1]);
    EXPECT(deliver(line) == 0, "http result");
    snprintf(text_expected, sizeof(text_expected), "result http.request %d 302 hi|2", g_ids[1]);
    expect_update(&next, text_expected);
    snprintf(line, sizeof(line), "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{}}", g_ids[2]);
    const int before = g_sent_count;
    EXPECT(deliver(line) == 0 && g_sent_count == before, "a silent request's success is not an event");
    snprintf(line, sizeof(line), "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{\"keys\":[\"a\",\"b\"]}}", g_ids[3]);
    EXPECT(deliver(line) == 0, "keys result");
    snprintf(text_expected, sizeof(text_expected), "result storage.keys %d 0 a\nb|3", g_ids[3]);
    expect_update(&next, text_expected);
    snprintf(line, sizeof(line), "{\"cap\":\"audio\",\"event\":\"sound_done\",\"data\":{\"request\":%d,\"detail\":\"finished\"}}", g_ids[2]);
    EXPECT(deliver(line) == 0, "sound_done");
    snprintf(text_expected, sizeof(text_expected), "sound %d finished", g_ids[2]);
    expect_update(&next, text_expected);
    snprintf(line, sizeof(line), "{\"id\":%d,\"status\":-5,\"error\":\"no_grant\",\"result\":{}}", g_ids[1]);
    EXPECT(deliver(line) == 0, "broker error");
    expect_update(&next, "error http.request -5 no_grant");
    EXPECT(g_error_request == (uint32_t)g_ids[1], "errors carry the request id");

    /* Audio streams (D28): open and close are requests; samples go through audio_write. */
    {
        const int16_t pcm[4] = {1, -2, 3, -4};
        static int16_t big[AITBASE_AUDIO_STREAM_FRAME_SAMPLES + 1];
        const int open_id = aitbase_audio_stream_open(g_p, 22050);
        EXPECT(open_id > 0, "stream_open returns a request id");
        req = take(&next);
        EXPECT(req && strcmp(str(req, "cap"), "audio") == 0 && strcmp(str(req, "op"), "stream_open") == 0 &&
               num(cJSON_GetObjectItem(req, "args"), "rate") == 22050, "audio.stream_open with the rate");
        cJSON_Delete(req);
        EXPECT(aitbase_audio_stream_write(g_p, pcm, 4) == 4 && g_audio_len == 8 && memcmp(g_audio, pcm, 8) == 0,
               "samples go to audio_write as bytes");
        g_audio_result = 0;
        EXPECT(aitbase_audio_stream_write(g_p, pcm, 0) == AITBASE_ERR_ARGUMENT &&
               aitbase_audio_stream_write(g_p, big, AITBASE_AUDIO_STREAM_FRAME_SAMPLES + 1) == AITBASE_ERR_ARGUMENT &&
               aitbase_audio_stream_write(g_p, NULL, 4) == AITBASE_ERR_ARGUMENT,
               "no samples, too many or none at all are refused before the host");
        EXPECT(aitbase_audio_stream_write(g_p, big, AITBASE_AUDIO_STREAM_FRAME_SAMPLES) == (int)AITBASE_AUDIO_STREAM_FRAME_SAMPLES,
               "a full frame is taken");
        g_audio_result = -3;
        EXPECT(aitbase_audio_stream_write(g_p, pcm, 4) == AITBASE_ERR_IO, "a closed channel is AITBASE_ERR_IO");
        g_audio_result = -8;
        EXPECT(aitbase_audio_stream_write(g_p, pcm, 4) == AITBASE_ERR_UNSUPPORTED, "no grant is AITBASE_ERR_UNSUPPORTED");
        g_audio_result = 0;
        snprintf(line, sizeof(line), "{\"id\":%d,\"status\":-3,\"error\":\"speaker_busy\",\"result\":{}}", open_id);
        EXPECT(deliver(line) == 0, "stream_open refused");
        expect_update(&next, "error audio.stream_open -3 speaker_busy");
        EXPECT(deliver("{\"cap\":\"audio\",\"event\":\"stream_state\",\"data\":{\"state\":\"paused\"}}") == 0, "paused");
        expect_update(&next, "stream paused ");
        EXPECT(deliver("{\"cap\":\"audio\",\"event\":\"stream_state\",\"data\":{\"state\":\"stopped\",\"reason\":\"idle_timeout\"}}") == 0,
               "stopped");
        expect_update(&next, "stream stopped idle_timeout");
        EXPECT(aitbase_audio_stream_close(g_p) > 0, "stream_close returns a request id");
        req = take(&next);
        EXPECT(req && strcmp(str(req, "op"), "stream_close") == 0, "audio.stream_close");
        cJSON_Delete(req);
    }

    /* D29: a background open is a stream_open request with background and the title. */
    {
        EXPECT(aitbase_audio_stream_open_background(g_p, 16000, "Night") > 0, "background open returns a request id");
        req = take(&next);
        const cJSON *args = req ? cJSON_GetObjectItem(req, "args") : NULL;
        EXPECT(req && strcmp(str(req, "op"), "stream_open") == 0 && cJSON_IsTrue(cJSON_GetObjectItem(args, "background")) &&
               strcmp(str(args, "title"), "Night") == 0 && num(args, "rate") == 16000,
               "a background open sends the rate, background and title");
        cJSON_Delete(req);
    }

    EXPECT(deliver("not json") == -1, "a malformed device message is a failure");
    EXPECT(aitbase_wasm_message(AITBASE_LIMIT_RX_MAX + 1) == -1, "a length past the receive buffer is a failure");

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
