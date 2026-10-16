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

/* Sends {"id","cap","op","args"} and remembers the op for its reply. Takes ownership of args. */
static int send_request(aitbase_plugin_t *plugin, const char *cap, const char *op, cJSON *args) {
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
    snprintf(plugin->pending[id % AITBASE_PENDING].op, sizeof(plugin->pending[0].op), "%s", op);
    const int rc = aitbase_transport_send(plugin, text, strlen(text));
    cJSON_free(text);
    return rc;
}

int aitbase_core_send_hello(aitbase_plugin_t *plugin) {
    cJSON *hello = cJSON_CreateObject();
    if (hello) {
        cJSON_AddNumberToObject(hello, "protocol", AITBASE_PROTOCOL_VERSION);
        cJSON_AddNumberToObject(hello, "api_version", AITBASE_PLUGIN_API_VERSION);
        cJSON_AddStringToObject(hello, "plugin", aitbase_plugin_id);
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
    };
    for (size_t i = 0; i < sizeof(kinds) / sizeof(kinds[0]); ++i) {
        if (strcmp(name, kinds[i].name) == 0) return kinds[i].kind;
    }
    return AITBASE_EVENT_NONE;
}

/* Turns one device message into an event. Returns 1 for an event, 0 for a message to skip
 * (a successful reply, or an event a newer device added), or a negative status. */
static int parse_message(aitbase_plugin_t *plugin, const char *line, size_t len, aitbase_event_t *out) {
    cJSON *msg = cJSON_ParseWithLength(line, len);
    if (!cJSON_IsObject(msg)) {
        cJSON_Delete(msg);
        return AITBASE_ERR_PROTOCOL;
    }
    memset(out, 0, sizeof(*out));
    int rc = 0;
    const cJSON *id = cJSON_GetObjectItemCaseSensitive(msg, "id");
    const cJSON *event = cJSON_GetObjectItemCaseSensitive(msg, "event");
    if (cJSON_IsNumber(id)) {
        const uint32_t reply_id = (uint32_t)id->valuedouble;
        const cJSON *status = cJSON_GetObjectItemCaseSensitive(msg, "status");
        const int code = cJSON_IsNumber(status) ? status->valueint : AITBASE_ERR_PROTOCOL;
        const char *op = plugin->pending[reply_id % AITBASE_PENDING].id == reply_id
                             ? plugin->pending[reply_id % AITBASE_PENDING].op : "";
        if (strcmp(op, "hello") == 0) {
            if (code == 0) {
                plugin->ready = 1;
            } else {
                char error[65];
                copy_field(error, sizeof(error), msg, "error");
                aitbase_log(plugin, AITBASE_LOG_ERROR, "device refused the plugin: %s", error);
                rc = AITBASE_ERR_PROTOCOL;
            }
        } else if (code != 0) {
            out->kind = AITBASE_EVENT_ERROR;
            out->status = code;
            snprintf(out->op, sizeof(out->op), "%s", op);
            copy_field(out->detail, sizeof(out->detail), msg, "error");
            rc = 1;
        }
    } else if (cJSON_IsString(event)) {
        const cJSON *data = cJSON_GetObjectItemCaseSensitive(msg, "data");
        out->kind = kind_from_name(event->valuestring);
        if (out->kind != AITBASE_EVENT_NONE) {
            copy_field(out->node, sizeof(out->node), data, "node");
            copy_field(out->action, sizeof(out->action), data, "action");
            copy_field(out->detail, sizeof(out->detail), data, "detail");
            rc = 1;
        }
    }
    cJSON_Delete(msg);
    return rc;
}

void aitbase_core_dispatch(aitbase_plugin_t *plugin, const aitbase_event_t *event) {
    aitbase_on_event(plugin, event);
}

int aitbase_core_handle_message(aitbase_plugin_t *plugin, const char *line, size_t len) {
    aitbase_event_t event;
    const int was_ready = plugin->ready;
    const int rc = len > 0 ? parse_message(plugin, line, len, &event) : 0;
    if (rc < 0) return rc;
    if (rc == 1 && was_ready) aitbase_core_dispatch(plugin, &event);
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
        default: return "none";
    }
}
