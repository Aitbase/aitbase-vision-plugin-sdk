/*
 * v0 transport for aitbase_plugin.h: the development bridge of a developer-enabled device.
 * Newline-delimited JSON over a Unix socket; see docs/dev-bridge-protocol.md.
 */
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif

#include "aitbase_plugin.h"

#include <errno.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/un.h>
#include <unistd.h>

#include "cJSON.h"

#define AITBASE_DEFAULT_ENDPOINT "/tmp/aitbase_plugin_dev.sock"
#define AITBASE_ENDPOINT_ENV "AITBASE_PLUGIN_ENDPOINT"
#define AITBASE_MAX_UI_BYTES 16384u
#define AITBASE_RX_BUFFER 4096u

struct aitbase_plugin {
    int fd;
    size_t used;
    char rx[AITBASE_RX_BUFFER];
};

static void copy_field(char *dst, size_t cap, const cJSON *obj, const char *name) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(obj, name);
    const char *value = cJSON_IsString(item) ? item->valuestring : "";
    snprintf(dst, cap, "%s", value);
}

static int send_all(aitbase_plugin_t *plugin, const char *data, size_t len) {
    while (len > 0) {
        ssize_t n = send(plugin->fd, data, len, MSG_NOSIGNAL);
        if (n < 0 && errno == EINTR) continue;
        if (n <= 0) return AITBASE_ERR_IO;
        data += n;
        len -= (size_t)n;
    }
    return AITBASE_OK;
}

/* Serializes obj as one request line and frees it. */
static int send_request(aitbase_plugin_t *plugin, cJSON *obj) {
    char *text = cJSON_PrintUnformatted(obj);
    cJSON_Delete(obj);
    if (!text) return AITBASE_ERR_NO_MEMORY;
    int rc = send_all(plugin, text, strlen(text));
    if (rc == AITBASE_OK) rc = send_all(plugin, "\n", 1);
    cJSON_free(text);
    return rc;
}

aitbase_plugin_t *aitbase_plugin_connect(const char *endpoint) {
    if (!endpoint) endpoint = getenv(AITBASE_ENDPOINT_ENV);
    if (!endpoint || !*endpoint) endpoint = AITBASE_DEFAULT_ENDPOINT;

    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    if (strlen(endpoint) >= sizeof(addr.sun_path)) return NULL;
    memcpy(addr.sun_path, endpoint, strlen(endpoint) + 1);

    int fd = socket(AF_UNIX, SOCK_STREAM, 0);
    if (fd < 0) return NULL;
    if (connect(fd, (struct sockaddr *)&addr, sizeof(addr)) != 0) {
        close(fd);
        return NULL;
    }
    aitbase_plugin_t *plugin = (aitbase_plugin_t *)calloc(1, sizeof(*plugin));
    if (!plugin) {
        close(fd);
        return NULL;
    }
    plugin->fd = fd;
    return plugin;
}

void aitbase_plugin_disconnect(aitbase_plugin_t *plugin) {
    if (!plugin) return;
    close(plugin->fd);
    free(plugin);
}

int aitbase_ui_open(aitbase_plugin_t *plugin, const char *ui_json, size_t len, uint32_t wait_ms) {
    if (!plugin || !ui_json || len == 0 || len > AITBASE_MAX_UI_BYTES) return AITBASE_ERR_ARGUMENT;
    cJSON *ui = cJSON_ParseWithLength(ui_json, len);
    if (!cJSON_IsObject(ui)) {
        cJSON_Delete(ui);
        return AITBASE_ERR_ARGUMENT;
    }
    if (wait_ms > AITBASE_UI_MAX_WAIT_MS) wait_ms = AITBASE_UI_MAX_WAIT_MS;
    cJSON *req = cJSON_CreateObject();
    if (!req) {
        cJSON_Delete(ui);
        return AITBASE_ERR_NO_MEMORY;
    }
    cJSON_AddStringToObject(req, "op", "open");
    cJSON_AddNumberToObject(req, "wait_ms", wait_ms);
    cJSON_AddItemToObject(req, "ui", ui);
    return send_request(plugin, req);
}

int aitbase_ui_open_file(aitbase_plugin_t *plugin, const char *path, uint32_t wait_ms) {
    if (!plugin || !path) return AITBASE_ERR_ARGUMENT;
    FILE *f = fopen(path, "rb");
    if (!f) return AITBASE_ERR_FILE;
    char *buf = (char *)malloc(AITBASE_MAX_UI_BYTES + 1);
    size_t n = buf ? fread(buf, 1, AITBASE_MAX_UI_BYTES + 1, f) : 0;
    fclose(f);
    int rc;
    if (!buf) {
        rc = AITBASE_ERR_NO_MEMORY;
    } else if (n == 0 || n > AITBASE_MAX_UI_BYTES) {
        rc = AITBASE_ERR_FILE;
    } else {
        rc = aitbase_ui_open(plugin, buf, n, wait_ms);
    }
    free(buf);
    return rc;
}

int aitbase_ui_update(aitbase_plugin_t *plugin, const char *node, const char *key,
                      const char *type, const char *value) {
    if (!plugin || !node || !key || !type || !value) return AITBASE_ERR_ARGUMENT;
    cJSON *req = cJSON_CreateObject();
    if (!req) return AITBASE_ERR_NO_MEMORY;
    cJSON_AddStringToObject(req, "op", "update");
    cJSON_AddStringToObject(req, "node", node);
    cJSON_AddStringToObject(req, "key", key);
    cJSON_AddStringToObject(req, "type", type);
    cJSON_AddStringToObject(req, "value", value);
    return send_request(plugin, req);
}

int aitbase_ui_set_text(aitbase_plugin_t *plugin, const char *node, const char *key,
                        const char *text) {
    return aitbase_ui_update(plugin, node, key, "string", text);
}

int aitbase_ui_close(aitbase_plugin_t *plugin) {
    if (!plugin) return AITBASE_ERR_ARGUMENT;
    cJSON *req = cJSON_CreateObject();
    if (!req) return AITBASE_ERR_NO_MEMORY;
    cJSON_AddStringToObject(req, "op", "close");
    return send_request(plugin, req);
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
 * (a successful reply or an unknown event from a newer device), or AITBASE_ERR_PROTOCOL. */
static int parse_message(const char *line, size_t len, aitbase_event_t *out) {
    cJSON *msg = cJSON_ParseWithLength(line, len);
    if (!cJSON_IsObject(msg)) {
        cJSON_Delete(msg);
        return AITBASE_ERR_PROTOCOL;
    }
    char ev[24];
    copy_field(ev, sizeof(ev), msg, "ev");
    memset(out, 0, sizeof(*out));
    int rc = 0;
    if (strcmp(ev, "reply") == 0) {
        const cJSON *status = cJSON_GetObjectItemCaseSensitive(msg, "status");
        if (cJSON_IsNumber(status) && status->valueint != 0) {
            out->kind = AITBASE_EVENT_ERROR;
            out->status = status->valueint;
            copy_field(out->op, sizeof(out->op), msg, "op");
            copy_field(out->detail, sizeof(out->detail), msg, "error");
            rc = 1;
        }
    } else {
        out->kind = kind_from_name(ev);
        if (out->kind != AITBASE_EVENT_NONE) {
            copy_field(out->node, sizeof(out->node), msg, "node");
            copy_field(out->action, sizeof(out->action), msg, "action");
            copy_field(out->detail, sizeof(out->detail), msg, "detail");
            rc = 1;
        }
    }
    cJSON_Delete(msg);
    return rc;
}

static long long now_ms(void) {
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (long long)tv.tv_sec * 1000 + tv.tv_usec / 1000;
}

int aitbase_plugin_next_event(aitbase_plugin_t *plugin, aitbase_event_t *out, int timeout_ms) {
    if (!plugin || !out) return AITBASE_ERR_ARGUMENT;
    const long long deadline = timeout_ms < 0 ? -1 : now_ms() + timeout_ms;
    for (;;) {
        char *newline;
        while ((newline = (char *)memchr(plugin->rx, '\n', plugin->used)) != NULL) {
            const size_t len = (size_t)(newline - plugin->rx);
            const int rc = len > 0 ? parse_message(plugin->rx, len, out) : 0;
            plugin->used -= len + 1;
            memmove(plugin->rx, newline + 1, plugin->used);
            if (rc != 0) return rc;
        }
        if (plugin->used == sizeof(plugin->rx)) return AITBASE_ERR_PROTOCOL;

        int wait = -1;
        if (deadline >= 0) {
            const long long left = deadline - now_ms();
            if (left <= 0) return 0;
            wait = (int)left;
        }
        struct pollfd pfd;
        pfd.fd = plugin->fd;
        pfd.events = POLLIN;
        pfd.revents = 0;
        const int ready = poll(&pfd, 1, wait);
        if (ready < 0 && errno == EINTR) continue;
        if (ready < 0) return AITBASE_ERR_IO;
        if (ready == 0) return 0;
        const ssize_t n = recv(plugin->fd, plugin->rx + plugin->used, sizeof(plugin->rx) - plugin->used, 0);
        if (n < 0 && errno == EINTR) continue;
        if (n <= 0) return AITBASE_ERR_IO;
        plugin->used += (size_t)n;
    }
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
        default: return "none";
    }
}
