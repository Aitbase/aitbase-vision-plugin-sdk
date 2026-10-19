/* Runs a real plugin through the development runtime against a fake device speaking protocol v1. */
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif

#include <fcntl.h>
#include <poll.h>
#include <stdarg.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <unistd.h>

#include "aitbase_plugin.h"
#include "aitbase_plugin_runtime.h"
#include "cJSON.h"

/* --- The plugin under test ------------------------------------------------------------------- */

AITBASE_PLUGIN_ID("test.plugin");

static int g_ticks = 0;

static void say(aitbase_plugin_t *p, const char *text) {
    aitbase_ui_set_text(p, "status.title", "title", text);
}

int aitbase_on_start(aitbase_plugin_t *p) {
    return aitbase_ui_open(p, "demo", 99999) == AITBASE_OK ? 0 : 1;
}

void aitbase_on_event(aitbase_plugin_t *p, const aitbase_event_t *ev) {
    char text[256];
    switch (ev->kind) {
        case AITBASE_EVENT_OPENED:
            say(p, "opened");
            aitbase_timer_set(p, 20);
            break;
        case AITBASE_EVENT_TIMER:
            snprintf(text, sizeof(text), "tick %d", ++g_ticks);
            say(p, text);
            if (g_ticks == 2) aitbase_timer_set(p, 0);
            break;
        case AITBASE_EVENT_ACTION:
            if (strcmp(ev->node, "template") == 0) {
                aitbase_template_t tpl;
                memset(&tpl, 0, sizeof(tpl));
                tpl.kind = "menu";
                tpl.title = "Pick";
                tpl.items[0] = "Tea";
                tpl.items[1] = "Coffee";
                aitbase_ui_template(p, &tpl, 45000);
                break;
            }
            if (strcmp(ev->node, "stream") == 0) {
                static const int16_t pcm[4] = {1, -2, 3, -4};
                snprintf(text, sizeof(text), "stream-write %d", aitbase_audio_stream_write(p, pcm, 4));
                say(p, text);
                break;
            }
            if (strcmp(ev->node, "alarm") == 0) {
                aitbase_schedule_set(p, "wake", 1900000000, 1, "Wake");
                aitbase_schedule_list(p);
                break;
            }
            if (strcmp(ev->node, "face") == 0) {
                aitbase_face_set(p, "face.1", "21 C");
                break;
            }
            if (strcmp(ev->node, "call") == 0 || strcmp(ev->node, "call_timeout") == 0) {
                /* A synchronous call (since 0.10): the result comes back here, before any event. */
                char result[128];
                aitbase_reply_t reply;
                memset(&reply, 0, sizeof(reply));
                reply.result = result;
                reply.result_cap = sizeof(result);
                const uint32_t timeout = strcmp(ev->node, "call") == 0 ? 2000 : 50;
                const int rc = aitbase_call(p, "settings", "get", NULL, timeout, &reply);
                snprintf(text, sizeof(text), "call %d %d %s", rc, reply.status, rc == AITBASE_OK ? result : "-");
                say(p, text);
                break;
            }
            snprintf(text, sizeof(text), "action %s %s", ev->node, ev->action);
            say(p, text);
            break;
        case AITBASE_EVENT_RESULT:
            if (strcmp(ev->op, "schedule.list") == 0) {
                snprintf(text, sizeof(text), "list %s", ev->text);
                say(p, text);
            }
            break;
        case AITBASE_EVENT_DEVICE:
            snprintf(text, sizeof(text), "device %s %s", ev->op, ev->text);
            say(p, text);
            break;
        case AITBASE_EVENT_TOOL_CALL: {
            /* Assistant tools (since 0.10, D31): the arguments through the getters, and their errors. */
            char city[16], tiny[3], other[8];
            long days = 0, wrong = 0;
            const int n = aitbase_tool_arg_string(ev, "city", city, sizeof(city));
            const int small = aitbase_tool_arg_string(ev, "city", tiny, sizeof(tiny));
            const int missing = aitbase_tool_arg_string(ev, "nope", other, sizeof(other));
            const int got_days = aitbase_tool_arg_int(ev, "days", &days);
            const int not_int = aitbase_tool_arg_int(ev, "city", &wrong);
            snprintf(text, sizeof(text), "%s %s %d %d %ld %d %d %d", ev->action, n >= 0 ? city : "-", n, got_days, days, small,
                     missing, not_int);
            aitbase_tool_reply(p, ev->request, text);
            break;
        }
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
    aitbase_log(p, AITBASE_LOG_INFO, "stopping after %d ticks", g_ticks);
}

/* --- The fake device ------------------------------------------------------------------------- */

static int g_failed = 0;
#define EXPECT(cond, msg)                                                    \
    do {                                                                     \
        if (!(cond)) {                                                       \
            fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, msg);    \
            ++g_failed;                                                      \
        }                                                                    \
    } while (0)

static char g_rx[32768];
static size_t g_used = 0;

/* Next request line from the plugin, parsed; NULL on timeout or EOF. */
static cJSON *next_request(int fd, int timeout_ms) {
    for (;;) {
        char *nl = memchr(g_rx, '\n', g_used);
        if (nl) {
            *nl = '\0';
            cJSON *req = cJSON_Parse(g_rx);
            size_t consumed = (size_t)(nl - g_rx) + 1;
            memmove(g_rx, nl + 1, g_used - consumed);
            g_used -= consumed;
            return req;
        }
        struct pollfd pfd = {fd, POLLIN, 0};
        if (poll(&pfd, 1, timeout_ms) <= 0) return NULL;
        ssize_t n = recv(fd, g_rx + g_used, sizeof(g_rx) - g_used, 0);
        if (n <= 0) return NULL;
        g_used += (size_t)n;
    }
}

static const char *str(const cJSON *obj, const char *name) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(obj, name);
    return cJSON_IsString(item) ? item->valuestring : "";
}

static int num(const cJSON *obj, const char *name) {
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(obj, name);
    return cJSON_IsNumber(item) ? item->valueint : -9999;
}

static void device_send(int fd, const char *fmt, ...) __attribute__((format(printf, 2, 3)));
static void device_send(int fd, const char *fmt, ...) {
    char line[512];
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(line, sizeof(line), fmt, ap);
    va_end(ap);
    strcat(line, "\n");
    if (send(fd, line, strlen(line), MSG_NOSIGNAL) < 0) ++g_failed;
}

static void reply_ok(int fd, const cJSON *req) {
    device_send(fd, "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{}}", num(req, "id"));
}

/* Reads one update request and checks its value; replies ok. Returns the request id. */
static int expect_update(int fd, const char *value) {
    cJSON *req = next_request(fd, 2000);
    const cJSON *args = cJSON_GetObjectItem(req, "args");
    int ok = req && strcmp(str(req, "cap"), "ui") == 0 && strcmp(str(req, "op"), "update") == 0 &&
             strcmp(str(args, "node"), "status.title") == 0 && strcmp(str(args, "type"), "string") == 0 &&
             strcmp(str(args, "value"), value) == 0;
    if (!ok) fprintf(stderr, "expected update \"%s\", got %s\n", value, req ? str(args, "value") : "nothing");
    EXPECT(ok, "update request");
    int id = num(req, "id");
    if (req) reply_ok(fd, req);
    cJSON_Delete(req);
    return id;
}

int main(void) {
    signal(SIGPIPE, SIG_IGN);
    alarm(20);
    char dir[] = "/tmp/aitbase-sdk-test-XXXXXX";
    if (!mkdtemp(dir)) return 2;
    char endpoint[96], ui_path[96];
    snprintf(endpoint, sizeof(endpoint), "%s/dev.sock", dir);
    snprintf(ui_path, sizeof(ui_path), "%s/demo.ui.json", dir);
    FILE *f = fopen(ui_path, "w");
    fputs("{\"format\":\"aitbase.plugin-ui\",\"version\":1,\"screen\":\"demo\"}\n", f);
    fclose(f);

    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    snprintf(addr.sun_path, sizeof(addr.sun_path), "%s", endpoint);
    int srv = socket(AF_UNIX, SOCK_STREAM, 0);
    if (srv < 0 || bind(srv, (struct sockaddr *)&addr, sizeof(addr)) != 0 || listen(srv, 1) != 0) return 2;

    pid_t child = fork();
    if (child == 0) {
        setenv("AITBASE_PLUGIN_ENDPOINT", endpoint, 1);
        setenv("AITBASE_PLUGIN_UI_DIR", dir, 1);
        char *args[] = {"plugin", NULL};
        _exit(aitbase_plugin_run(1, args));
    }
    int dev = accept(srv, NULL, NULL);

    /* Handshake first: nothing else may be sent before the device accepts the plugin. */
    cJSON *req = next_request(dev, 2000);
    const cJSON *args = cJSON_GetObjectItem(req, "args");
    EXPECT(req && strcmp(str(req, "cap"), "system") == 0 && strcmp(str(req, "op"), "hello") == 0, "hello first");
    EXPECT(num(args, "protocol") == 1 && num(args, "api_version") == AITBASE_PLUGIN_API_VERSION, "hello versions");
    EXPECT(strcmp(str(args, "plugin"), "test.plugin") == 0, "hello carries the plugin id");
    cJSON *early = next_request(dev, 100);
    EXPECT(early == NULL, "on_start waits for the handshake reply");
    cJSON_Delete(early);
    device_send(dev, "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{\"protocol\":1}}", num(req, "id"));
    cJSON_Delete(req);

    /* on_start opens the screen by name; the runtime embeds the compiled document. */
    req = next_request(dev, 2000);
    args = cJSON_GetObjectItem(req, "args");
    EXPECT(req && strcmp(str(req, "op"), "open") == 0 && strcmp(str(args, "screen"), "demo") == 0, "open by name");
    EXPECT(num(args, "wait_ms") == (int)AITBASE_UI_MAX_WAIT_MS, "wait is clamped");
    EXPECT(strcmp(str(cJSON_GetObjectItem(args, "document"), "format"), "aitbase.plugin-ui") == 0, "document embedded");
    if (req) reply_ok(dev, req);
    cJSON_Delete(req);

    /* Unknown events from newer devices are ignored; known ones reach the callback. */
    device_send(dev, "{\"cap\":\"ui\",\"event\":\"from_a_newer_device\",\"data\":{}}");
    device_send(dev, "{\"cap\":\"ui\",\"event\":\"opened\",\"data\":{\"node\":\"\",\"action\":\"\",\"detail\":\"\"}}");
    expect_update(dev, "opened");
    expect_update(dev, "tick 1");
    int last = expect_update(dev, "tick 2");
    cJSON *extra = next_request(dev, 100);
    EXPECT(extra == NULL, "timer stops when set to 0");
    cJSON_Delete(extra);

    device_send(dev, "{\"cap\":\"ui\",\"event\":\"action\",\"data\":{\"node\":\"increment\",\"action\":\"plugin.increment\",\"detail\":\"\"}}");
    expect_update(dev, "action increment plugin.increment");

    /* aitbase_call: the plugin waits for its reply; an event that arrives first is delivered after
     * the call returns, as AITBASE_EVENT_DEVICE for a capability without typed events. */
    device_send(dev, "{\"cap\":\"ui\",\"event\":\"action\",\"data\":{\"node\":\"call\",\"action\":\"x\",\"detail\":\"\"}}");
    req = next_request(dev, 2000);
    EXPECT(req && strcmp(str(req, "cap"), "settings") == 0 && strcmp(str(req, "op"), "get") == 0, "aitbase_call sends its request");
    device_send(dev, "{\"cap\":\"schedule\",\"event\":\"due\",\"data\":{\"id\":\"wake\"}}");
    if (req) device_send(dev, "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{\"found\":true}}", num(req, "id"));
    cJSON_Delete(req);
    expect_update(dev, "call 0 0 {\"found\":true}");
    expect_update(dev, "device schedule.due {\"id\":\"wake\"}");

    /* Assistant tools (since 0.10, D31): tool.call reaches the plugin as AITBASE_EVENT_TOOL_CALL and
     * aitbase_tool_reply answers it; a refused reply is an ERROR for tool.reply. */
    device_send(dev, "{\"cap\":\"tool\",\"event\":\"call\",\"data\":{\"call\":7,\"tool\":\"today\",\"args\":{\"city\":\"Hanoi\",\"days\":3}}}");
    req = next_request(dev, 2000);
    {
        const cJSON *rargs = cJSON_GetObjectItem(req, "args");
        EXPECT(req && strcmp(str(req, "cap"), "tool") == 0 && strcmp(str(req, "op"), "reply") == 0 && num(rargs, "call") == 7,
               "aitbase_tool_reply answers the call by its id");
        EXPECT(strcmp(str(rargs, "text"), "today Hanoi 5 0 3 -5 -1 -1") == 0,
               "the getters read the string and the int, and report a short buffer, a missing name and a wrong type");
    }
    if (req) device_send(dev, "{\"id\":%d,\"status\":-1,\"error\":\"tool_unknown\",\"result\":{}}", num(req, "id"));
    cJSON_Delete(req);
    expect_update(dev, "error tool.reply -1 tool_unknown");
    device_send(dev, "{\"cap\":\"tool\",\"event\":\"call\",\"data\":{\"tool\":\"today\",\"args\":{}}}");

    /* aitbase_ui_template: the device's own layout with the plugin's text (since 0.10). */
    device_send(dev, "{\"cap\":\"ui\",\"event\":\"action\",\"data\":{\"node\":\"template\",\"action\":\"x\",\"detail\":\"\"}}");
    req = next_request(dev, 2000);
    {
        const cJSON *targs = cJSON_GetObjectItem(req, "args");
        EXPECT(req && strcmp(str(req, "cap"), "ui") == 0 && strcmp(str(req, "op"), "template") == 0 &&
                   strcmp(str(targs, "template"), "menu") == 0 && strcmp(str(targs, "title"), "Pick") == 0 &&
                   strcmp(str(targs, "item1"), "Tea") == 0 && strcmp(str(targs, "item2"), "Coffee") == 0 &&
                   cJSON_GetObjectItem(targs, "item3") == NULL && num(targs, "wait_ms") == 30000,
               "aitbase_ui_template sends ui.template with the items in order and the wait capped");
    }
    if (req) reply_ok(dev, req);
    cJSON_Delete(req);

    /* aitbase_schedule_*: community alarms (since 0.10, D26), tracked like storage. */
    device_send(dev, "{\"cap\":\"ui\",\"event\":\"action\",\"data\":{\"node\":\"alarm\",\"action\":\"x\",\"detail\":\"\"}}");
    req = next_request(dev, 2000);
    {
        const cJSON *sargs = cJSON_GetObjectItem(req, "args");
        EXPECT(req && strcmp(str(req, "cap"), "schedule") == 0 && strcmp(str(req, "op"), "set") == 0 &&
                   strcmp(str(sargs, "id"), "wake") == 0 && num(sargs, "due_epoch") == 1900000000 &&
                   cJSON_IsTrue(cJSON_GetObjectItem(sargs, "rings")) && strcmp(str(sargs, "label"), "Wake") == 0,
               "aitbase_schedule_set sends schedule.set");
    }
    if (req) reply_ok(dev, req);
    cJSON_Delete(req);
    req = next_request(dev, 2000);
    EXPECT(req && strcmp(str(req, "op"), "list") == 0, "aitbase_schedule_list sends schedule.list");
    if (req) {
        device_send(dev, "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{\"entries\":[{\"id\":\"wake\",\"due_epoch\":1900000000,\"rings\":true,\"label\":\"Wake\"}]}}",
                    num(req, "id"));
    }
    cJSON_Delete(req);
    expect_update(dev, "list [{\"id\":\"wake\",\"due_epoch\":1900000000,\"rings\":true,\"label\":\"Wake\"}]");

    /* aitbase_face_set: the plugin's idle face values (since 0.10). */
    device_send(dev, "{\"cap\":\"ui\",\"event\":\"action\",\"data\":{\"node\":\"face\",\"action\":\"x\",\"detail\":\"\"}}");
    req = next_request(dev, 2000);
    {
        const cJSON *fargs = cJSON_GetObjectItem(req, "args");
        EXPECT(req && strcmp(str(req, "cap"), "face") == 0 && strcmp(str(req, "op"), "set") == 0 &&
                   strcmp(str(fargs, "key"), "face.1") == 0 && strcmp(str(fargs, "value"), "21 C") == 0,
               "aitbase_face_set sends face.set");
    }
    if (req) reply_ok(dev, req);
    cJSON_Delete(req);

    /* No reply in time: AITBASE_ERR_TIMEOUT; the late reply is ignored. */
    device_send(dev, "{\"cap\":\"ui\",\"event\":\"action\",\"data\":{\"node\":\"call_timeout\",\"action\":\"x\",\"detail\":\"\"}}");
    req = next_request(dev, 2000);
    EXPECT(req && strcmp(str(req, "cap"), "settings") == 0, "a second call");
    expect_update(dev, "call -7 0 -");
    if (req) device_send(dev, "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{}}", num(req, "id"));
    cJSON_Delete(req);
    /* A device event of a known capability keeps its own kind: a module action is not a UI action. */
    device_send(dev, "{\"cap\":\"module\",\"event\":\"action\",\"data\":{\"action\":\"snooze\"}}");
    expect_update(dev, "device module.action {\"action\":\"snooze\"}");

    /* A failed reply becomes an ERROR event naming the request's op. */
    device_send(dev, "{\"id\":%d,\"status\":-1,\"error\":\"bad_request\",\"result\":{}}", last);
    expect_update(dev, "error update -1 bad_request");

    /* Preemption: the plugin stops, the runtime closes the screen and exits cleanly. */
    device_send(dev, "{\"cap\":\"ui\",\"event\":\"preempted\",\"data\":{\"node\":\"\",\"action\":\"\",\"detail\":\"\"}}");
    req = next_request(dev, 2000);
    EXPECT(req && strcmp(str(req, "op"), "close") == 0, "close on stop");
    cJSON_Delete(req);
    int status = 0;
    waitpid(child, &status, 0);
    EXPECT(WIFEXITED(status) && WEXITSTATUS(status) == 0, "plugin exits 0");

    close(dev);
    close(srv);
    unlink(endpoint);

    /* Supervised: the device's plugin supervisor hands the plugin an inherited socket. */
    int sv[2];
    if (socketpair(AF_UNIX, SOCK_STREAM, 0, sv) != 0) return 2;
    child = fork();
    if (child == 0) {
        char fd_text[16];
        close(sv[0]);
        snprintf(fd_text, sizeof(fd_text), "%d", sv[1]);
        unsetenv("AITBASE_PLUGIN_ENDPOINT");
        setenv("AITBASE_PLUGIN_FD", fd_text, 1);
        setenv("AITBASE_PLUGIN_UI_DIR", dir, 1);
        char *args[] = {"plugin", NULL};
        _exit(aitbase_plugin_run(1, args));
    }
    close(sv[1]);
    g_used = 0;
    req = next_request(sv[0], 2000);
    EXPECT(req && strcmp(str(req, "op"), "hello") == 0, "supervised plugin says hello on the inherited socket");
#ifdef __linux__
    {
        /* A non-dumpable process's /proc files belong to root. */
        char proc_path[64];
        struct stat st;
        snprintf(proc_path, sizeof(proc_path), "/proc/%d/environ", (int)child);
        EXPECT(getuid() == 0 || (stat(proc_path, &st) == 0 && st.st_uid == 0), "supervised plugin is not dumpable");
    }
#endif
    if (req) device_send(sv[0], "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{\"protocol\":1}}", num(req, "id"));
    cJSON_Delete(req);
    req = next_request(sv[0], 2000);
    EXPECT(req && strcmp(str(req, "op"), "open") == 0, "supervised plugin opens its screen");
    if (req) reply_ok(sv[0], req);
    cJSON_Delete(req);
    device_send(sv[0], "{\"cap\":\"ui\",\"event\":\"preempted\",\"data\":{\"node\":\"\",\"action\":\"\",\"detail\":\"\"}}");
    req = next_request(sv[0], 2000);
    EXPECT(req && strcmp(str(req, "op"), "close") == 0, "supervised plugin closes on stop");
    cJSON_Delete(req);
    waitpid(child, &status, 0);
    EXPECT(WIFEXITED(status) && WEXITSTATUS(status) == 0, "supervised plugin exits 0");
    close(sv[0]);

    /* D28: with the audio channel on fd 4, samples go there as length-prefixed frames; without
     * it, writes are unsupported. */
    for (int with_audio = 0; with_audio < 2; ++with_audio) {
        int av[2] = {-1, -1};
        if (socketpair(AF_UNIX, SOCK_STREAM, 0, sv) != 0) return 2;
        if (with_audio && socketpair(AF_UNIX, SOCK_STREAM, 0, av) != 0) return 2;
        child = fork();
        if (child == 0) {
            close(sv[0]);
            if (av[0] >= 0) close(av[0]);
            /* The channel above fd 4, and fd 4 the audio channel or nothing. */
            const int channel = fcntl(sv[1], F_DUPFD, 5);
            if (with_audio) dup2(av[1], 4);
            else close(4);
            char fd_text[16];
            snprintf(fd_text, sizeof(fd_text), "%d", channel);
            unsetenv("AITBASE_PLUGIN_ENDPOINT");
            setenv("AITBASE_PLUGIN_FD", fd_text, 1);
            setenv("AITBASE_PLUGIN_UI_DIR", dir, 1);
            char *args[] = {"plugin", NULL};
            _exit(aitbase_plugin_run(1, args));
        }
        close(sv[1]);
        if (av[1] >= 0) close(av[1]);
        g_used = 0;
        req = next_request(sv[0], 2000);
        if (req) device_send(sv[0], "{\"id\":%d,\"status\":0,\"error\":\"\",\"result\":{\"protocol\":1}}", num(req, "id"));
        cJSON_Delete(req);
        req = next_request(sv[0], 2000);
        if (req) reply_ok(sv[0], req);
        cJSON_Delete(req);
        device_send(sv[0], "{\"cap\":\"ui\",\"event\":\"action\",\"data\":{\"node\":\"stream\",\"action\":\"x\"}}");
        req = next_request(sv[0], 2000);
        const char *want = with_audio ? "stream-write 4" : "stream-write -8";
        EXPECT(req && strcmp(str(cJSON_GetObjectItem(req, "args"), "value"), want) == 0,
               with_audio ? "a supervised plugin with fd 4 writes samples" : "without fd 4, writes are AITBASE_ERR_UNSUPPORTED");
        cJSON_Delete(req);
        if (with_audio) {
            unsigned char frame[16];
            struct pollfd pfd = {av[0], POLLIN, 0};
            EXPECT(poll(&pfd, 1, 2000) == 1 && recv(av[0], frame, sizeof(frame), 0) == 12 &&
                   memcmp(frame, "\x08\x00\x00\x00", 4) == 0 && frame[4] == 1 && frame[6] == 0xfe,
                   "the frame has its 4-byte little-endian length and the samples");
            close(av[0]);
        }
        device_send(sv[0], "{\"cap\":\"ui\",\"event\":\"preempted\",\"data\":{}}");
        req = next_request(sv[0], 2000);
        cJSON_Delete(req);
        waitpid(child, &status, 0);
        close(sv[0]);
    }

    /* A descriptor that isn't a socket is refused; the plugin doesn't fall back to the path. */
    child = fork();
    if (child == 0) {
        setenv("AITBASE_PLUGIN_FD", "0", 1);
        setenv("AITBASE_PLUGIN_ENDPOINT", "/nonexistent/should-not-be-used.sock", 1);
        char *args[] = {"plugin", NULL};
        _exit(aitbase_plugin_run(1, args));
    }
    waitpid(child, &status, 0);
    EXPECT(WIFEXITED(status) && WEXITSTATUS(status) == 1, "non-socket AITBASE_PLUGIN_FD is refused");

    unlink(ui_path);
    rmdir(dir);
    if (g_failed) {
        fprintf(stderr, "aitbase_plugin_runtime_test: %d failure(s)\n", g_failed);
        return 1;
    }
    fprintf(stderr, "aitbase_plugin_runtime_test: PASS\n");
    return 0;
}
