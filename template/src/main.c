/*
 * __PLUGIN_NAME__: shows "Count: N" and adds 1 each time the user presses RIGHT on the button.
 * Stops when the device closes or takes back the screen (for example a LEFT long press).
 */
#include <stdio.h>
#include <string.h>

#include "aitbase_plugin.h"

AITBASE_PLUGIN_ID("__PLUGIN_ID__"); /* must match "id" in plugin.json */

/* Names from ui/screens/main.xml: <card name="status"> holds <lv_label name="title">. */
#define SCREEN "main"
#define TITLE_NODE "status.title"
#define TITLE_SUBJECT "title"
#define INCREMENT_ACTION "plugin.increment"

static int g_count = 0;

static void show_count(aitbase_plugin_t *plugin) {
    char text[32];
    snprintf(text, sizeof(text), "Count: %d", g_count);
    aitbase_ui_set_text(plugin, TITLE_NODE, TITLE_SUBJECT, text);
}

int aitbase_on_start(aitbase_plugin_t *plugin) {
    /* Wait up to 30 s if the device is busy, e.g. the voice assistant is talking. */
    return aitbase_ui_open(plugin, SCREEN, AITBASE_UI_MAX_WAIT_MS);
}

void aitbase_on_event(aitbase_plugin_t *plugin, const aitbase_event_t *event) {
    switch (event->kind) {
        case AITBASE_EVENT_OPENED:
            show_count(plugin);
            break;
        case AITBASE_EVENT_ACTION:
            if (strcmp(event->action, INCREMENT_ACTION) == 0) {
                ++g_count;
                aitbase_log(plugin, AITBASE_LOG_INFO, "count %d", g_count);
                show_count(plugin);
            }
            break;
        case AITBASE_EVENT_ERROR:
            aitbase_log(plugin, AITBASE_LOG_ERROR, "%s failed (%d %s)", event->op, event->status, event->detail);
            if (strcmp(event->op, "open") == 0) aitbase_plugin_stop(plugin);
            break;
        case AITBASE_EVENT_REJECTED:
        case AITBASE_EVENT_CLOSED:
        case AITBASE_EVENT_PREEMPTED:
            aitbase_plugin_stop(plugin);
            break;
        default:
            break;
    }
}
