/*
 * Weather: answers the voice assistant (assistant tools, D31). It has no screen. When the user asks
 * "what's the weather in Hanoi", the assistant calls its "today" tool; the device starts the plugin
 * if it isn't running, and the plugin replies with one line from wttr.in within 5 s.
 */
#include <stdio.h>
#include <string.h>

#include "aitbase_plugin.h"

AITBASE_PLUGIN_ID("example.weather"); /* must match "id" in plugin.json */

/* The call being answered: its id, and the HTTP request made for it. */
static uint32_t g_call = 0;
static int g_request = 0;

/* Percent-encodes city for the URL path; returns 0 when it doesn't fit. */
static int encode(const char *city, char *out, size_t cap) {
    static const char kHex[] = "0123456789ABCDEF";
    size_t n = 0;
    for (const unsigned char *c = (const unsigned char *)city; *c; ++c) {
        const int plain = (*c >= 'a' && *c <= 'z') || (*c >= 'A' && *c <= 'Z') || (*c >= '0' && *c <= '9') ||
                          *c == '-' || *c == '.' || *c == '_';
        if (n + (plain ? 1 : 3) >= cap) return 0;
        if (plain) {
            out[n++] = (char)*c;
        } else {
            out[n++] = '%';
            out[n++] = kHex[*c >> 4];
            out[n++] = kHex[*c & 15];
        }
    }
    out[n] = '\0';
    return 1;
}

static void answer(aitbase_plugin_t *plugin, const char *text) {
    if (g_call) aitbase_tool_reply(plugin, g_call, text);
    g_call = 0;
    g_request = 0;
}

int aitbase_on_start(aitbase_plugin_t *plugin) {
    /* Nothing to show: started any other way than for the assistant, stop at once. */
    if (strcmp(aitbase_trigger(plugin), "assistant") != 0) aitbase_plugin_stop(plugin);
    return 0;
}

void aitbase_on_event(aitbase_plugin_t *plugin, const aitbase_event_t *event) {
    switch (event->kind) {
        case AITBASE_EVENT_TOOL_CALL: {
            char city[41], path[128], url[192];
            if (strcmp(event->action, "today") != 0 ||
                aitbase_tool_arg_string(event, "city", city, sizeof(city)) < 0 || !encode(city, path, sizeof(path))) {
                aitbase_tool_reply(plugin, event->request, "I can't look that up.");
                break;
            }
            g_call = event->request;
            /* format=3: one line such as "Hanoi: +31°C". */
            snprintf(url, sizeof(url), "https://wttr.in/%s?format=3", path);
            g_request = aitbase_http_get(plugin, url);
            if (g_request < 0) answer(plugin, "The weather service can't be reached now.");
            break;
        }
        case AITBASE_EVENT_RESULT:
            if (event->request == (uint32_t)g_request) {
                answer(plugin, event->code == 200 && event->text_len > 0 ? event->text : "No forecast for that city.");
            }
            break;
        case AITBASE_EVENT_ERROR:
            aitbase_log(plugin, AITBASE_LOG_ERROR, "%s failed (%d %s)", event->op, event->status, event->detail);
            if (g_request > 0 && event->request == (uint32_t)g_request) answer(plugin, "The weather service can't be reached now.");
            break;
        default:
            break;
    }
}
