/*
 * Protocol core shared by the native and WASM runtimes. Protocol: docs/plugin-protocol.md.
 */
#include "aitbase_plugin_core.h"

#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"

static void copy_field(char *dst, size_t cap, const cJSON *obj, const char *name) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(obj, name);
    snprintf(dst, cap, "%s", cJSON_IsString(item) ? item->valuestring : "");
}

/* Sends {"id","cap","op","args"} and remembers the op for its reply. Takes ownership of args.
 * Returns the request id, or a negative status. tracked requests report their id in events; their
 * op is remembered as "<cap>.<op>". */
static int send_tracked(aitbase_plugin_t *plugin, const char *cap, const char *op, cJSON *args, int tracked) {
    if (!args) return AITBASE_ERR_NO_MEMORY;
    cJSON *req = cJSON_CreateObject();
    if (!req) {
        cJSON_Delete(args);
        return AITBASE_ERR_NO_MEMORY;
    }
    const uint32_t id = ++plugin->next_id;
    cJSON_AddNumberToObject(req, "id", id);
    cJSON_AddStringToObject(req, "cap", cap);
    cJSON_AddStringToObject(req, "op", op);
    cJSON_AddItemToObject(req, "args", args);
    char *text = cJSON_PrintUnformatted(req);
    cJSON_Delete(req);
    if (!text) return AITBASE_ERR_NO_MEMORY;
    plugin->pending[id % AITBASE_PENDING].id = id;
    plugin->pending[id % AITBASE_PENDING].tracked = tracked;
    if (tracked) {
        snprintf(plugin->pending[id % AITBASE_PENDING].op, sizeof(plugin->pending[0].op), "%s.%s", cap, op);
    } else {
        snprintf(plugin->pending[id % AITBASE_PENDING].op, sizeof(plugin->pending[0].op), "%s", op);
    }
    const int rc = aitbase_transport_send(plugin, text, strlen(text));
    cJSON_free(text);
    return rc == AITBASE_OK ? (int)id : rc;
}

/* Screen and system requests: 0 or a negative status. */
static int send_request(aitbase_plugin_t *plugin, const char *cap, const char *op, cJSON *args) {
    const int rc = send_tracked(plugin, cap, op, args, 0);
    return rc > 0 ? AITBASE_OK : rc;
}

int aitbase_core_send_hello(aitbase_plugin_t *plugin) {
    cJSON *hello = cJSON_CreateObject();
    if (hello) {
        cJSON_AddNumberToObject(hello, "protocol", AITBASE_PROTOCOL_VERSION);
        cJSON_AddNumberToObject(hello, "api_version", AITBASE_PLUGIN_API_VERSION);
        cJSON_AddStringToObject(hello, "plugin", aitbase_plugin_id);
        cJSON_AddNumberToObject(hello, "rx_max", AITBASE_RX_BUFFER);
    }
    return send_request(plugin, "system", "hello", hello);
}

static int valid_screen_name(const char *name) {
    if (!name || !*name || strlen(name) > 32 || name[0] < 'a' || name[0] > 'z') return 0;
    for (const char *c = name; *c; ++c) {
        if (!((*c >= 'a' && *c <= 'z') || (*c >= '0' && *c <= '9') || *c == '_')) return 0;
    }
    return 1;
}

/* Loads the compiled screen through the transport and parses it. */
static cJSON *load_screen(aitbase_plugin_t *plugin, const char *screen, int *status) {
    char *buf = (char *)malloc(AITBASE_MAX_UI_BYTES);
    size_t n = 0;
    cJSON *doc = NULL;
    if (!buf) {
        *status = AITBASE_ERR_NO_MEMORY;
        return NULL;
    }
    *status = aitbase_transport_read_screen(plugin, screen, buf, AITBASE_MAX_UI_BYTES, &n);
    if (*status == AITBASE_OK && (n == 0 || !cJSON_IsObject(doc = cJSON_ParseWithLength(buf, n)))) {
        cJSON_Delete(doc);
        doc = NULL;
        *status = AITBASE_ERR_ARGUMENT;
    }
    free(buf);
    return doc;
}

int aitbase_ui_open(aitbase_plugin_t *plugin, const char *screen, uint32_t wait_ms) {
    if (!plugin || !valid_screen_name(screen)) return AITBASE_ERR_ARGUMENT;
    int status = AITBASE_OK;
    cJSON *doc = load_screen(plugin, screen, &status);
    if (!doc) return status;
    cJSON *args = cJSON_CreateObject();
    if (!args) {
        cJSON_Delete(doc);
        return AITBASE_ERR_NO_MEMORY;
    }
    cJSON_AddStringToObject(args, "screen", screen);
    cJSON_AddNumberToObject(args, "wait_ms", wait_ms > AITBASE_UI_MAX_WAIT_MS ? AITBASE_UI_MAX_WAIT_MS : wait_ms);
    cJSON_AddItemToObject(args, "document", doc);
    return send_request(plugin, "ui", "open", args);
}

int aitbase_ui_update(aitbase_plugin_t *plugin, const char *node, const char *key,
                      const char *type, const char *value) {
    if (!plugin || !node || !key || !type || !value) return AITBASE_ERR_ARGUMENT;
    cJSON *args = cJSON_CreateObject();
    if (!args) return AITBASE_ERR_NO_MEMORY;
    cJSON_AddStringToObject(args, "node", node);
    cJSON_AddStringToObject(args, "key", key);
    cJSON_AddStringToObject(args, "type", type);
    cJSON_AddStringToObject(args, "value", value);
    return send_request(plugin, "ui", "update", args);
}

int aitbase_ui_set_text(aitbase_plugin_t *plugin, const char *node, const char *key,
                        const char *text) {
    return aitbase_ui_update(plugin, node, key, "string", text);
}

int aitbase_ui_close(aitbase_plugin_t *plugin) {
    if (!plugin) return AITBASE_ERR_ARGUMENT;
    return send_request(plugin, "ui", "close", cJSON_CreateObject());
}

/* --- Broker requests (docs/capabilities.md) --------------------------------------------------- */

static int storage_request(aitbase_plugin_t *plugin, const char *op, const char *key, const char *value) {
    if (!plugin || (key && !*key)) return AITBASE_ERR_ARGUMENT;
    cJSON *args = cJSON_CreateObject();
    if (args && key) cJSON_AddStringToObject(args, "key", key);
    if (args && value) cJSON_AddStringToObject(args, "value", value);
    return send_tracked(plugin, "storage", op, args, 1);
}

int aitbase_storage_get(aitbase_plugin_t *plugin, const char *key) {
    return key ? storage_request(plugin, "get", key, NULL) : AITBASE_ERR_ARGUMENT;
}

int aitbase_storage_set(aitbase_plugin_t *plugin, const char *key, const char *value) {
    return key && value ? storage_request(plugin, "set", key, value) : AITBASE_ERR_ARGUMENT;
}

int aitbase_storage_delete(aitbase_plugin_t *plugin, const char *key) {
    return key ? storage_request(plugin, "delete", key, NULL) : AITBASE_ERR_ARGUMENT;
}

int aitbase_storage_keys(aitbase_plugin_t *plugin) {
    return storage_request(plugin, "keys", NULL, NULL);
}

int aitbase_http_request(aitbase_plugin_t *plugin, const aitbase_http_request_t *request) {
    if (!plugin || !request || !request->url) return AITBASE_ERR_ARGUMENT;
    cJSON *args = cJSON_CreateObject();
    if (!args) return AITBASE_ERR_NO_MEMORY;
    cJSON_AddStringToObject(args, "method", request->method ? request->method : "GET");
    cJSON_AddStringToObject(args, "url", request->url);
    if (request->header_count > 0) {
        cJSON *headers = cJSON_AddObjectToObject(args, "headers");
        for (size_t i = 0; headers && i < request->header_count; ++i) {
            const char *h = request->headers ? request->headers[i] : NULL;
            const char *colon = h ? strchr(h, ':') : NULL;
            char name[128];
            if (!colon || colon == h || (size_t)(colon - h) >= sizeof(name)) {
                cJSON_Delete(args);
                return AITBASE_ERR_ARGUMENT;
            }
            memcpy(name, h, (size_t)(colon - h));
            name[colon - h] = '\0';
            const char *value = colon + 1;
            while (*value == ' ' || *value == '\t') ++value;
            cJSON_AddStringToObject(headers, name, value);
        }
    }
    if (request->body) cJSON_AddStringToObject(args, "body", request->body);
    if (request->timeout_ms) cJSON_AddNumberToObject(args, "timeout_ms", request->timeout_ms);
    return send_tracked(plugin, "http", "request", args, 1);
}

int aitbase_http_get(aitbase_plugin_t *plugin, const char *url) {
    aitbase_http_request_t request;
    memset(&request, 0, sizeof(request));
    request.url = url;
    return aitbase_http_request(plugin, &request);
}

int aitbase_audio_tone(aitbase_plugin_t *plugin, uint32_t freq_hz, uint32_t duration_ms, uint32_t volume) {
    if (!plugin) return AITBASE_ERR_ARGUMENT;
    cJSON *args = cJSON_CreateObject();
    if (args) {
        cJSON_AddNumberToObject(args, "freq_hz", freq_hz);
        cJSON_AddNumberToObject(args, "duration_ms", duration_ms);
        cJSON_AddNumberToObject(args, "volume", volume > 100 ? 100 : volume);
    }
    return send_tracked(plugin, "audio", "tone", args, 1);
}

int aitbase_audio_play(aitbase_plugin_t *plugin, const char *sound, uint32_t volume) {
    if (!plugin || !valid_screen_name(sound)) return AITBASE_ERR_ARGUMENT;
    cJSON *args = cJSON_CreateObject();
    if (args) {
        cJSON_AddStringToObject(args, "sound", sound);
        cJSON_AddNumberToObject(args, "volume", volume > 100 ? 100 : volume);
    }
    return send_tracked(plugin, "audio", "play", args, 1);
}

int aitbase_audio_stop(aitbase_plugin_t *plugin) {
    if (!plugin) return AITBASE_ERR_ARGUMENT;
    return send_tracked(plugin, "audio", "stop", cJSON_CreateObject(), 1);
}

void aitbase_plugin_stop(aitbase_plugin_t *plugin) {
    if (plugin) plugin->stop = 1;
}

void aitbase_log(aitbase_plugin_t *plugin, aitbase_log_level_t level, const char *fmt, ...) {
    char line[256];
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(line, sizeof(line), fmt, ap);
    va_end(ap);
    (void)plugin;
    aitbase_transport_log(level, line);
}

static aitbase_event_kind_t kind_from_name(const char *name) {
    static const struct {
        const char *name;
        aitbase_event_kind_t kind;
    } kinds[] = {
        {"opened", AITBASE_EVENT_OPENED},     {"rejected", AITBASE_EVENT_REJECTED},
        {"closed", AITBASE_EVENT_CLOSED},     {"preempted", AITBASE_EVENT_PREEMPTED},
        {"action", AITBASE_EVENT_ACTION},     {"update_rejected", AITBASE_EVENT_UPDATE_REJECTED},
        {"sound_done", AITBASE_EVENT_SOUND_DONE}, {"key", AITBASE_EVENT_KEY},
    };
    for (size_t i = 0; i < sizeof(kinds) / sizeof(kinds[0]); ++i) {
        if (strcmp(name, kinds[i].name) == 0) return kinds[i].kind;
    }
    return AITBASE_EVENT_NONE;
}

static const char *string_or_empty(const cJSON *obj, const char *name) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(obj, name);
    return cJSON_IsString(item) ? item->valuestring : "";
}

/* Fills a RESULT from a successful tracked reply. *keys receives an allocated newline-separated
 * list for storage.keys; the caller frees it after dispatch. */
static void fill_result(const cJSON *result, const char *op, aitbase_event_t *out, char **keys) {
    out->kind = AITBASE_EVENT_RESULT;
    if (strcmp(op, "storage.get") == 0) {
        out->code = cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(result, "found"));
        out->text = string_or_empty(result, "value");
    } else if (strcmp(op, "storage.keys") == 0) {
        const cJSON *list = cJSON_GetObjectItemCaseSensitive(result, "keys");
        const cJSON *key = NULL;
        size_t total = 1;
        cJSON_ArrayForEach(key, list) {
            if (cJSON_IsString(key)) total += strlen(key->valuestring) + 1;
        }
        *keys = (char *)malloc(total);
        if (*keys) {
            char *at = *keys;
            cJSON_ArrayForEach(key, list) {
                if (!cJSON_IsString(key)) continue;
                if (at != *keys) *at++ = '\n';
                const size_t n = strlen(key->valuestring);
                memcpy(at, key->valuestring, n);
                at += n;
            }
            *at = '\0';
            out->text = *keys;
        }
    } else if (strcmp(op, "http.request") == 0) {
        const cJSON *status = cJSON_GetObjectItemCaseSensitive(result, "status");
        out->code = cJSON_IsNumber(status) ? status->valueint : 0;
        out->text = string_or_empty(result, "body");
        out->truncated = cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(result, "truncated"));
        out->location = string_or_empty(result, "location");
    } else {
        out->kind = AITBASE_EVENT_NONE; /* success of a silent request (set, delete, tone...) */
    }
}

void aitbase_core_dispatch(aitbase_plugin_t *plugin, const aitbase_event_t *event) {
    aitbase_event_t copy = *event;
    if (!copy.text) copy.text = "";
    if (!copy.location) copy.location = "";
    copy.text_len = strlen(copy.text);
    aitbase_on_event(plugin, &copy);
}

int aitbase_core_handle_message(aitbase_plugin_t *plugin, const char *line, size_t len) {
    if (len == 0) return AITBASE_OK;
    cJSON *msg = cJSON_ParseWithLength(line, len);
    if (!cJSON_IsObject(msg)) {
        cJSON_Delete(msg);
        return AITBASE_ERR_PROTOCOL;
    }
    const int was_ready = plugin->ready;
    aitbase_event_t out;
    char *keys = NULL;
    int rc = AITBASE_OK;
    memset(&out, 0, sizeof(out));
    const cJSON *id = cJSON_GetObjectItemCaseSensitive(msg, "id");
    const cJSON *event = cJSON_GetObjectItemCaseSensitive(msg, "event");
    if (cJSON_IsNumber(id)) {
        const uint32_t reply_id = (uint32_t)id->valuedouble;
        const cJSON *status = cJSON_GetObjectItemCaseSensitive(msg, "status");
        const int code = cJSON_IsNumber(status) ? status->valueint : AITBASE_ERR_PROTOCOL;
        const int known = plugin->pending[reply_id % AITBASE_PENDING].id == reply_id;
        const char *op = known ? plugin->pending[reply_id % AITBASE_PENDING].op : "";
        const int tracked = known && plugin->pending[reply_id % AITBASE_PENDING].tracked;
        if (strcmp(op, "hello") == 0) {
            if (code == 0) {
                plugin->ready = 1;
            } else {
                aitbase_log(plugin, AITBASE_LOG_ERROR, "device refused the plugin: %s", string_or_empty(msg, "error"));
                rc = AITBASE_ERR_PROTOCOL;
            }
        } else if (code != 0) {
            out.kind = AITBASE_EVENT_ERROR;
            out.status = code;
            out.request = tracked ? reply_id : 0;
            snprintf(out.op, sizeof(out.op), "%s", op);
            copy_field(out.detail, sizeof(out.detail), msg, "error");
        } else if (tracked) {
            out.request = reply_id;
            snprintf(out.op, sizeof(out.op), "%s", op);
            fill_result(cJSON_GetObjectItemCaseSensitive(msg, "result"), op, &out, &keys);
        }
    } else if (cJSON_IsString(event)) {
        const cJSON *data = cJSON_GetObjectItemCaseSensitive(msg, "data");
        out.kind = kind_from_name(event->valuestring);
        if (out.kind != AITBASE_EVENT_NONE) {
            const cJSON *request = cJSON_GetObjectItemCaseSensitive(data, "request");
            copy_field(out.node, sizeof(out.node), data, "node");
            copy_field(out.action, sizeof(out.action), data, "action");
            copy_field(out.detail, sizeof(out.detail), data, "detail");
            out.request = cJSON_IsNumber(request) ? (uint32_t)request->valuedouble : 0;
            if (out.kind == AITBASE_EVENT_KEY) {
                const char *key = string_or_empty(data, "key");
                const char *gesture = string_or_empty(data, "gesture");
                out.key = strcmp(key, "left") == 0 ? AITBASE_KEY_LEFT : strcmp(key, "right") == 0 ? AITBASE_KEY_RIGHT : AITBASE_KEY_NONE;
                out.gesture = strcmp(gesture, "press") == 0        ? AITBASE_GESTURE_PRESS
                              : strcmp(gesture, "long_press") == 0 ? AITBASE_GESTURE_LONG_PRESS
                                                                   : AITBASE_GESTURE_NONE;
                /* A button or gesture from a newer device: ignore it, like an unknown event. */
                if (out.key == AITBASE_KEY_NONE || out.gesture == AITBASE_GESTURE_NONE) out.kind = AITBASE_EVENT_NONE;
            }
        }
    }
    /* Strings in the event point into msg, which lives until the callback returns. */
    if (rc == AITBASE_OK && out.kind != AITBASE_EVENT_NONE && was_ready) aitbase_core_dispatch(plugin, &out);
    free(keys);
    cJSON_Delete(msg);
    if (rc != AITBASE_OK) return rc;
    if (!was_ready && plugin->ready && aitbase_on_start(plugin) != 0) plugin->stop = 1;
    return AITBASE_OK;
}

void aitbase_core_finish(aitbase_plugin_t *plugin) {
    if (plugin->finished) return;
    plugin->finished = 1;
    if (plugin->ready) (void)aitbase_ui_close(plugin);
    aitbase_on_stop(plugin); /* no-op default in aitbase_plugin_defaults.c */
}

const char *aitbase_event_kind_name(aitbase_event_kind_t kind) {
    switch (kind) {
        case AITBASE_EVENT_OPENED: return "opened";
        case AITBASE_EVENT_REJECTED: return "rejected";
        case AITBASE_EVENT_CLOSED: return "closed";
        case AITBASE_EVENT_PREEMPTED: return "preempted";
        case AITBASE_EVENT_ACTION: return "action";
        case AITBASE_EVENT_UPDATE_REJECTED: return "update_rejected";
        case AITBASE_EVENT_ERROR: return "error";
        case AITBASE_EVENT_TIMER: return "timer";
        case AITBASE_EVENT_RESULT: return "result";
        case AITBASE_EVENT_SOUND_DONE: return "sound_done";
        case AITBASE_EVENT_KEY: return "key";
        default: return "none";
    }
}
