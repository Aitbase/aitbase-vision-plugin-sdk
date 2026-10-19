/*
 * Development runtime for aitbase_plugin.h: runs a plugin as a native process connected to a
 * developer-enabled device over its plugin socket, or to the device's plugin supervisor over an
 * inherited socket. The protocol itself is in aitbase_plugin_core.c (docs/plugin-protocol.md).
 */
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif

#include "aitbase_plugin.h"
#include "aitbase_plugin_core.h"
#include "aitbase_plugin_runtime.h"
#include "cJSON.h"

#include <errno.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/un.h>
#include <unistd.h>
#ifdef __linux__
#include <sys/prctl.h>
#endif

#define AITBASE_DEFAULT_ENDPOINT "/tmp/aitbase_plugin_dev.sock"
#define AITBASE_ENDPOINT_ENV "AITBASE_PLUGIN_ENDPOINT"
#define AITBASE_UI_DIR_ENV "AITBASE_PLUGIN_UI_DIR"
/* Set by the device's plugin supervisor: the inherited connection descriptor. */
#define AITBASE_CHANNEL_FD_ENV "AITBASE_PLUGIN_FD"
#define AITBASE_HELLO_TIMEOUT_MS 3000
#define AITBASE_IDLE_POLL_MS 200

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

int aitbase_transport_send(aitbase_plugin_t *plugin, const char *message, size_t len) {
    int rc = send_all(plugin, message, len);
    if (rc == AITBASE_OK) rc = send_all(plugin, "\n", 1);
    return rc;
}

void aitbase_transport_log(aitbase_log_level_t level, const char *text) {
    static const char *const names[] = {"error", "warn", "info", "debug"};
    fprintf(stderr, "[%s] %s: %s\n", aitbase_plugin_id, (unsigned)level < 4 ? names[level] : "log", text);
}

/* Reads <ui_dir>/<screen>.ui.json, the compiler output for ui/screens/<screen>.xml. */
int aitbase_transport_read_screen(aitbase_plugin_t *plugin, const char *screen, char *buf, size_t cap,
                                  size_t *len) {
    char path[sizeof(plugin->ui_dir) + 48];
    char extra;
    snprintf(path, sizeof(path), "%s/%s.ui.json", plugin->ui_dir, screen);
    FILE *f = fopen(path, "rb");
    if (!f) return AITBASE_ERR_NOT_FOUND;
    *len = fread(buf, 1, cap, f);
    const int too_big = *len == cap && fread(&extra, 1, 1, f) == 1;
    fclose(f);
    return too_big ? AITBASE_ERR_ARGUMENT : AITBASE_OK;
}

int aitbase_timer_set(aitbase_plugin_t *plugin, uint32_t interval_ms) {
    if (!plugin) return AITBASE_ERR_ARGUMENT;
    if (interval_ms > 0 && interval_ms < AITBASE_MIN_TIMER_MS) interval_ms = AITBASE_MIN_TIMER_MS;
    plugin->timer_ms = interval_ms;
    plugin->timer_due = interval_ms ? now_ms() + interval_ms : 0;
    return AITBASE_OK;
}

int aitbase_transport_can_wait(void) { return 1; }

/* Waits for the reply to request id (aitbase_call). Complete lines that aren't it stay in the
 * receive buffer, in order, for drain_messages() once the current callback returns. */
int aitbase_transport_take_reply(aitbase_plugin_t *plugin, uint32_t id, uint32_t timeout_ms, struct cJSON **reply) {
    const long long deadline = now_ms() + timeout_ms;
    for (;;) {
        size_t off = 0;
        char *newline;
        while ((newline = (char *)memchr(plugin->rx + off, '\n', plugin->used - off)) != NULL) {
            const size_t len = (size_t)(newline - (plugin->rx + off));
            cJSON *msg = cJSON_ParseWithLength(plugin->rx + off, len);
            const cJSON *msg_id = cJSON_GetObjectItemCaseSensitive(msg, "id");
            if (cJSON_IsNumber(msg_id) && (uint32_t)msg_id->valuedouble == id &&
                cJSON_GetObjectItemCaseSensitive(msg, "status")) {
                memmove(plugin->rx + off, newline + 1, plugin->used - off - len - 1);
                plugin->used -= len + 1;
                *reply = msg;
                return AITBASE_OK;
            }
            cJSON_Delete(msg);
            off += len + 1;
        }
        if (plugin->used == sizeof(plugin->rx)) return AITBASE_ERR_PROTOCOL;
        const long long left = deadline - now_ms();
        if (left <= 0) return AITBASE_ERR_TIMEOUT;
        struct pollfd pfd;
        pfd.fd = plugin->fd;
        pfd.events = POLLIN;
        pfd.revents = 0;
        const int ready = poll(&pfd, 1, (int)left);
        if (ready < 0 && errno == EINTR) continue;
        if (ready <= 0) continue;
        const ssize_t n = recv(plugin->fd, plugin->rx + plugin->used, sizeof(plugin->rx) - plugin->used, 0);
        if (n < 0 && errno == EINTR) continue;
        if (n <= 0) return AITBASE_ERR_IO;
        plugin->used += (size_t)n;
    }
}

/* Handles every complete line in the receive buffer. Returns 0 or a negative status. */
static int drain_messages(aitbase_plugin_t *plugin) {
    char *newline;
    while ((newline = (char *)memchr(plugin->rx, '\n', plugin->used)) != NULL) {
        const size_t len = (size_t)(newline - plugin->rx);
        const int rc = aitbase_core_handle_message(plugin, plugin->rx, len);
        plugin->used -= len + 1;
        memmove(plugin->rx, newline + 1, plugin->used);
        if (rc < 0) return rc;
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

/* Under the device's plugin supervisor the connection is an inherited socket, not a path. */
static int inherited_channel(void) {
    const char *value = getenv(AITBASE_CHANNEL_FD_ENV);
    char *end = NULL;
    struct stat st;
    long fd;
    if (!value || !*value) return -2;
    fd = strtol(value, &end, 10);
    if (*end || fd < 0 || fd > 1024 || fstat((int)fd, &st) != 0 || !S_ISSOCK(st.st_mode)) return -1;
#ifdef __linux__
    /* exec reset this; a supervised plugin must not be traceable by its own UID. */
    prctl(PR_SET_DUMPABLE, 0, 0, 0, 0);
#endif
    return (int)fd;
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
    const int inherited = inherited_channel();
    if (inherited == -1) {
        aitbase_log(plugin, AITBASE_LOG_ERROR, AITBASE_CHANNEL_FD_ENV " is not a connected socket");
        free(plugin);
        return 1;
    }
    if (inherited >= 0) {
        plugin->fd = inherited;
    } else {
        const char *endpoint = getenv(AITBASE_ENDPOINT_ENV);
        plugin->fd = connect_endpoint(endpoint && *endpoint ? endpoint : AITBASE_DEFAULT_ENDPOINT);
    }
    if (plugin->fd < 0) {
        aitbase_log(plugin, AITBASE_LOG_ERROR, "cannot reach the device plugin endpoint "
                    "(developer firmware with the plugin bridge required)");
        free(plugin);
        return 1;
    }

    int status = aitbase_core_send_hello(plugin) == AITBASE_OK ? 0 : 1;
    const long long hello_deadline = now_ms() + AITBASE_HELLO_TIMEOUT_MS;

    while (status == 0 && !plugin->stop && !g_signal) {
        /* Messages an aitbase_call() set aside while it waited come first. */
        if (plugin->used && memchr(plugin->rx, '\n', plugin->used)) {
            if (drain_messages(plugin) < 0) status = 1;
            continue;
        }
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
                aitbase_core_dispatch(plugin, &tick);
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

    aitbase_core_finish(plugin);
    close(plugin->fd);
    free(plugin);
    return status;
}
