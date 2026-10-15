/*
 * Development runtime for aitbase_plugin.h: runs a plugin as a native process connected to a
 * developer-enabled device over its plugin socket. Protocol: docs/plugin-protocol.md.
 */
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif

#include "aitbase_plugin.h"
#include "aitbase_plugin_runtime.h"

#include <errno.h>
#include <poll.h>
#include <signal.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/un.h>
#include <unistd.h>

#include "cJSON.h"

#define AITBASE_PROTOCOL_VERSION 1
#define AITBASE_DEFAULT_ENDPOINT "/tmp/aitbase_plugin_dev.sock"
#define AITBASE_ENDPOINT_ENV "AITBASE_PLUGIN_ENDPOINT"
#define AITBASE_UI_DIR_ENV "AITBASE_PLUGIN_UI_DIR"
#define AITBASE_MAX_UI_BYTES 16384u
#define AITBASE_RX_BUFFER 4096u
#define AITBASE_PENDING 32u
#define AITBASE_HELLO_TIMEOUT_MS 3000
#define AITBASE_IDLE_POLL_MS 200
#define AITBASE_MIN_TIMER_MS 10u

struct aitbase_plugin {
    int fd;
    int ready;
    int stop;
    uint32_t next_id;
    struct {
        uint32_t id;
        char op[16];
    } pending[AITBASE_PENDING];
    uint32_t timer_ms;
    long long timer_due;
    size_t used;
    char rx[AITBASE_RX_BUFFER];
    char ui_dir[256];
};

static volatile sig_atomic_t g_signal = 0;

static void on_signal(int sig) {
    (void)sig;
    g_signal = 1;
}

static long long now_ms(void) {
    struct timeval tv;
    gettimeofday(&tv, NULL);
    return (long long)tv.tv_sec * 1000 + tv.tv_usec / 1000;
}

static void copy_field(char *dst, size_t cap, const cJSON *obj, const char *name) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(obj, name);
    snprintf(dst, cap, "%s", cJSON_IsString(item) ? item->valuestring : "");
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
    int rc = send_all(plugin, text, strlen(text));
    if (rc == AITBASE_OK) rc = send_all(plugin, "\n", 1);
    cJSON_free(text);
    return rc;
}

static int valid_screen_name(const char *name) {
    if (!name || !*name || strlen(name) > 32 || name[0] < 'a' || name[0] > 'z') return 0;
    for (const char *c = name; *c; ++c) {
        if (!((*c >= 'a' && *c <= 'z') || (*c >= '0' && *c <= '9') || *c == '_')) return 0;
    }
    return 1;
}

/* Reads <ui_dir>/<screen>.ui.json, the compiler output for ui/screens/<screen>.xml. */
static cJSON *load_screen(aitbase_plugin_t *plugin, const char *screen, int *status) {
    char path[sizeof(plugin->ui_dir) + 48];
    snprintf(path, sizeof(path), "%s/%s.ui.json", plugin->ui_dir, screen);
    FILE *f = fopen(path, "rb");
    if (!f) {
        *status = AITBASE_ERR_NOT_FOUND;
        return NULL;
    }
    char *buf = (char *)malloc(AITBASE_MAX_UI_BYTES + 1);
    size_t n = buf ? fread(buf, 1, AITBASE_MAX_UI_BYTES + 1, f) : 0;
    fclose(f);
    cJSON *doc = NULL;
    if (!buf) {
        *status = AITBASE_ERR_NO_MEMORY;
    } else if (n == 0 || n > AITBASE_MAX_UI_BYTES || !cJSON_IsObject(doc = cJSON_ParseWithLength(buf, n))) {
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

int aitbase_timer_set(aitbase_plugin_t *plugin, uint32_t interval_ms) {
    if (!plugin) return AITBASE_ERR_ARGUMENT;
    if (interval_ms > 0 && interval_ms < AITBASE_MIN_TIMER_MS) interval_ms = AITBASE_MIN_TIMER_MS;
    plugin->timer_ms = interval_ms;
    plugin->timer_due = interval_ms ? now_ms() + interval_ms : 0;
    return AITBASE_OK;
}

void aitbase_plugin_stop(aitbase_plugin_t *plugin) {
    if (plugin) plugin->stop = 1;
}

void aitbase_log(aitbase_plugin_t *plugin, aitbase_log_level_t level, const char *fmt, ...) {
    static const char *const names[] = {"error", "warn", "info", "debug"};
    char line[256];
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(line, sizeof(line), fmt, ap);
    va_end(ap);
    (void)plugin;
    fprintf(stderr, "[%s] %s: %s\n", aitbase_plugin_id, (unsigned)level < 4 ? names[level] : "log", line);
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

static void dispatch(aitbase_plugin_t *plugin, const aitbase_event_t *event) {
    aitbase_on_event(plugin, event);
}

/* Handles every complete line in the receive buffer. Returns 0 or a negative status. */
static int drain_messages(aitbase_plugin_t *plugin) {
    char *newline;
    while ((newline = (char *)memchr(plugin->rx, '\n', plugin->used)) != NULL) {
        const size_t len = (size_t)(newline - plugin->rx);
        aitbase_event_t event;
        const int was_ready = plugin->ready;
        int rc = len > 0 ? parse_message(plugin, plugin->rx, len, &event) : 0;
        plugin->used -= len + 1;
        memmove(plugin->rx, newline + 1, plugin->used);
        if (rc < 0) return rc;
        if (rc == 1 && was_ready) dispatch(plugin, &event);
        if (!was_ready && plugin->ready && aitbase_on_start(plugin) != 0) plugin->stop = 1;
        if (plugin->stop) return 0;
    }
    return plugin->used == sizeof(plugin->rx) ? AITBASE_ERR_PROTOCOL : 0;
}

static int connect_endpoint(const char *endpoint) {
    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    if (strlen(endpoint) >= sizeof(addr.sun_path)) return -1;
    memcpy(addr.sun_path, endpoint, strlen(endpoint) + 1);
    int fd = socket(AF_UNIX, SOCK_STREAM, 0);
    if (fd < 0) return -1;
    if (connect(fd, (struct sockaddr *)&addr, sizeof(addr)) != 0) {
        close(fd);
        return -1;
    }
    return fd;
}

static void set_ui_dir(aitbase_plugin_t *plugin, const char *argv0) {
    const char *dir = getenv(AITBASE_UI_DIR_ENV);
    if (dir && *dir) {
        snprintf(plugin->ui_dir, sizeof(plugin->ui_dir), "%s", dir);
        return;
    }
    /* Default: next to the plugin binary, where the build puts <screen>.ui.json. */
    const char *slash = argv0 ? strrchr(argv0, '/') : NULL;
    if (slash) {
        snprintf(plugin->ui_dir, sizeof(plugin->ui_dir), "%.*s", (int)(slash - argv0), argv0);
    } else {
        snprintf(plugin->ui_dir, sizeof(plugin->ui_dir), ".");
    }
}

int aitbase_plugin_run(int argc, char **argv) {
    (void)argc;
    signal(SIGINT, on_signal);
    signal(SIGTERM, on_signal);
    signal(SIGPIPE, SIG_IGN);

    aitbase_plugin_t *plugin = (aitbase_plugin_t *)calloc(1, sizeof(*plugin));
    if (!plugin) return 1;
    set_ui_dir(plugin, argv ? argv[0] : NULL);
    const char *endpoint = getenv(AITBASE_ENDPOINT_ENV);
    plugin->fd = connect_endpoint(endpoint && *endpoint ? endpoint : AITBASE_DEFAULT_ENDPOINT);
    if (plugin->fd < 0) {
        aitbase_log(plugin, AITBASE_LOG_ERROR, "cannot reach the device plugin endpoint "
                    "(developer firmware with the plugin bridge required)");
        free(plugin);
        return 1;
    }

    cJSON *hello = cJSON_CreateObject();
    if (hello) {
        cJSON_AddNumberToObject(hello, "protocol", AITBASE_PROTOCOL_VERSION);
        cJSON_AddNumberToObject(hello, "api_version", AITBASE_PLUGIN_API_VERSION);
        cJSON_AddStringToObject(hello, "plugin", aitbase_plugin_id);
    }
    int status = send_request(plugin, "system", "hello", hello) == AITBASE_OK ? 0 : 1;
    const long long hello_deadline = now_ms() + AITBASE_HELLO_TIMEOUT_MS;

    while (status == 0 && !plugin->stop && !g_signal) {
        int wait = AITBASE_IDLE_POLL_MS;
        const long long now = now_ms();
        if (!plugin->ready) {
            if (now >= hello_deadline) {
                aitbase_log(plugin, AITBASE_LOG_ERROR, "device did not answer the handshake");
                status = 1;
                break;
            }
            if (hello_deadline - now < wait) wait = (int)(hello_deadline - now);
        } else if (plugin->timer_ms) {
            if (now >= plugin->timer_due) {
                aitbase_event_t tick;
                memset(&tick, 0, sizeof(tick));
                tick.kind = AITBASE_EVENT_TIMER;
                plugin->timer_due = now + plugin->timer_ms;
                dispatch(plugin, &tick);
                continue;
            }
            if (plugin->timer_due - now < wait) wait = (int)(plugin->timer_due - now);
        }
        struct pollfd pfd;
        pfd.fd = plugin->fd;
        pfd.events = POLLIN;
        pfd.revents = 0;
        const int ready = poll(&pfd, 1, wait);
        if (ready < 0 && errno == EINTR) continue;
        if (ready < 0) {
            status = 1;
            break;
        }
        if (ready == 0) continue;
        const ssize_t n = recv(plugin->fd, plugin->rx + plugin->used, sizeof(plugin->rx) - plugin->used, 0);
        if (n < 0 && errno == EINTR) continue;
        if (n <= 0) {
            aitbase_log(plugin, AITBASE_LOG_ERROR, "connection to the device closed");
            status = 1;
            break;
        }
        plugin->used += (size_t)n;
        if (drain_messages(plugin) < 0) status = 1;
    }

    if (plugin->ready) (void)aitbase_ui_close(plugin);
    aitbase_on_stop(plugin); /* no-op default in aitbase_plugin_defaults.c */
    close(plugin->fd);
    free(plugin);
    return status;
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
