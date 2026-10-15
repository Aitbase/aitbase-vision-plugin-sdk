/* Exercises the v0 client against an in-process fake device that speaks the dev-bridge protocol. */
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif

#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

#include "aitbase_plugin.h"
#include "cJSON.h"

static int g_failed = 0;

#define EXPECT(cond, msg)                                   \
    do {                                                    \
        if (!(cond)) {                                      \
            fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, msg); \
            ++g_failed;                                     \
        }                                                   \
    } while (0)

/* Reads one request line written by the client and returns it parsed. */
static cJSON *read_request(int fd) {
    char line[20000];
    size_t used = 0;
    while (used < sizeof(line) - 1) {
        ssize_t n = recv(fd, line + used, 1, 0);
        if (n <= 0) return NULL;
        if (line[used] == '\n') break;
        ++used;
    }
    line[used] = '\0';
    return cJSON_Parse(line);
}

static void device_send(int fd, const char *text) {
    if (send(fd, text, strlen(text), MSG_NOSIGNAL) != (ssize_t)strlen(text)) {
        fprintf(stderr, "device send failed\n");
        exit(1);
    }
}

static const char *str(const cJSON *obj, const char *name) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(obj, name);
    return cJSON_IsString(item) ? item->valuestring : "";
}

int main(void) {
    signal(SIGPIPE, SIG_IGN);
    char dir[] = "/tmp/aitbase-sdk-test-XXXXXX";
    if (!mkdtemp(dir)) return 2;
    char endpoint[96];
    char ui_path[96];
    snprintf(endpoint, sizeof(endpoint), "%s/dev.sock", dir);
    snprintf(ui_path, sizeof(ui_path), "%s/ui.json", dir);

    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    snprintf(addr.sun_path, sizeof(addr.sun_path), "%s", endpoint);
    int srv = socket(AF_UNIX, SOCK_STREAM, 0);
    if (srv < 0 || bind(srv, (struct sockaddr *)&addr, sizeof(addr)) != 0 || listen(srv, 1) != 0) return 2;

    EXPECT(aitbase_plugin_connect("/nonexistent/aitbase.sock") == NULL, "missing endpoint fails");
    aitbase_plugin_t *plugin = aitbase_plugin_connect(endpoint);
    EXPECT(plugin != NULL, "connect to endpoint");
    if (!plugin) return 1;
    int dev = accept(srv, NULL, NULL);

    /* Requests carry the documented wire fields. */
    FILE *f = fopen(ui_path, "w");
    fputs("{\"format\":\"aitbase.plugin-ui\",\"version\":1}\n", f);
    fclose(f);
    EXPECT(aitbase_ui_open_file(plugin, ui_path, 99999) == AITBASE_OK, "open from file");
    cJSON *req = read_request(dev);
    EXPECT(strcmp(str(req, "op"), "open") == 0, "open op");
    EXPECT(cJSON_GetObjectItem(req, "wait_ms")->valueint == (int)AITBASE_UI_MAX_WAIT_MS, "wait is clamped");
    EXPECT(strcmp(str(cJSON_GetObjectItem(req, "ui"), "format"), "aitbase.plugin-ui") == 0, "ui is embedded as an object");
    cJSON_Delete(req);

    EXPECT(aitbase_ui_set_text(plugin, "status.title", "title", "Count: 1") == AITBASE_OK, "set_text");
    req = read_request(dev);
    EXPECT(strcmp(str(req, "op"), "update") == 0 && strcmp(str(req, "type"), "string") == 0 &&
               strcmp(str(req, "value"), "Count: 1") == 0 && strcmp(str(req, "node"), "status.title") == 0,
           "update fields");
    cJSON_Delete(req);

    EXPECT(aitbase_ui_close(plugin) == AITBASE_OK, "close");
    req = read_request(dev);
    EXPECT(strcmp(str(req, "op"), "close") == 0, "close op");
    cJSON_Delete(req);

    EXPECT(aitbase_ui_open(plugin, "{not json", 9, 0) == AITBASE_ERR_ARGUMENT, "invalid UI JSON is refused locally");
    EXPECT(aitbase_ui_open_file(plugin, "/nonexistent/ui.json", 0) == AITBASE_ERR_FILE, "missing UI file");

    /* Successful replies and unknown future events are skipped; failures become ERROR events. */
    aitbase_event_t ev;
    device_send(dev, "{\"ev\":\"reply\",\"op\":\"open\",\"status\":0,\"gen\":1,\"error\":\"\"}\n"
                     "{\"ev\":\"from_a_newer_device\",\"gen\":1}\n"
                     "{\"ev\":\"opened\",\"gen\":1,\"node\":\"\",\"action\":\"\",\"detail\":\"\"}\n"
                     "{\"ev\":\"reply\",\"op\":\"update\",\"status\":-1,\"gen\":1,\"error\":\"bad_request\"}\n");
    EXPECT(aitbase_plugin_next_event(plugin, &ev, 1000) == 1 && ev.kind == AITBASE_EVENT_OPENED, "opened event");
    EXPECT(aitbase_plugin_next_event(plugin, &ev, 1000) == 1 && ev.kind == AITBASE_EVENT_ERROR &&
               strcmp(ev.op, "update") == 0 && ev.status == -1 && strcmp(ev.detail, "bad_request") == 0,
           "failed reply becomes an error event");

    /* A message split across reads is reassembled; a partial line alone times out. */
    device_send(dev, "{\"ev\":\"action\",\"gen\":1,\"node\":\"increment\",");
    EXPECT(aitbase_plugin_next_event(plugin, &ev, 50) == 0, "partial message times out");
    device_send(dev, "\"action\":\"plugin.increment\",\"detail\":\"\"}\n");
    EXPECT(aitbase_plugin_next_event(plugin, &ev, 1000) == 1 && ev.kind == AITBASE_EVENT_ACTION &&
               strcmp(ev.node, "increment") == 0 && strcmp(ev.action, "plugin.increment") == 0,
           "action event");
    EXPECT(strcmp(aitbase_event_kind_name(ev.kind), "action") == 0, "event names");

    device_send(dev, "{\"ev\":\"preempted\",\"gen\":1,\"node\":\"\",\"action\":\"\",\"detail\":\"\"}\n");
    EXPECT(aitbase_plugin_next_event(plugin, &ev, 1000) == 1 && ev.kind == AITBASE_EVENT_PREEMPTED, "preempted event");

    close(dev);
    EXPECT(aitbase_plugin_next_event(plugin, &ev, 1000) == AITBASE_ERR_IO, "device disconnect is reported");

    aitbase_plugin_disconnect(plugin);
    close(srv);
    unlink(endpoint);
    unlink(ui_path);
    rmdir(dir);
    if (g_failed) {
        fprintf(stderr, "aitbase_plugin_client_test: %d failure(s)\n", g_failed);
        return 1;
    }
    fprintf(stderr, "aitbase_plugin_client_test: PASS\n");
    return 0;
}
