/*
 * Preferences: a switch, a slider and a roller (input widgets, SDK 0.12). LEFT moves the focus,
 * RIGHT toggles the switch or puts the slider or roller in edit mode, where LEFT and RIGHT step and
 * a RIGHT long press confirms. Each change arrives as AITBASE_EVENT_CHANGED and is saved, so the
 * next start shows the same values.
 */
#include <stdio.h>
#include <string.h>

#include "aitbase_plugin.h"

AITBASE_PLUGIN_ID("example.prefs"); /* must match "id" in plugin.json */

static const char *const kCities[] = {"Hanoi", "Hue", "Da Nang", "Saigon"};

/* The widgets' values, in the order of ui/screens/main.xml: sound, level, city. */
static int g_sound = 0, g_level = 5, g_city = 0;
static int g_loading = 0; /* the storage.get request for the saved values */

static void show(aitbase_plugin_t *plugin) {
    char text[64];
    snprintf(text, sizeof(text), "%s, level %d, %s", g_sound ? "Sound on" : "Muted", g_level, kCities[g_city]);
    aitbase_ui_set_text(plugin, "summary", "title", text);
}

/* Sets each widget through its binding: that sends no AITBASE_EVENT_CHANGED back. */
static void show_all(aitbase_plugin_t *plugin) {
    char text[16];
    snprintf(text, sizeof(text), "%d", g_sound);
    aitbase_ui_set_text(plugin, "sound", "value.1", text);
    snprintf(text, sizeof(text), "%d", g_level);
    aitbase_ui_set_text(plugin, "level", "value.2", text);
    snprintf(text, sizeof(text), "%d", g_city);
    aitbase_ui_set_text(plugin, "city", "value.3", text);
    show(plugin);
}

int aitbase_on_start(aitbase_plugin_t *plugin) {
    return aitbase_ui_open(plugin, "main", AITBASE_UI_MAX_WAIT_MS);
}

void aitbase_on_event(aitbase_plugin_t *plugin, const aitbase_event_t *event) {
    char saved[32];
    switch (event->kind) {
        case AITBASE_EVENT_OPENED:
            g_loading = aitbase_storage_get(plugin, "prefs");
            show(plugin);
            break;
        case AITBASE_EVENT_RESULT:
            if (event->request == (uint32_t)g_loading && event->code == 1) {
                int sound, level, city;
                if (sscanf(event->text, "%d,%d,%d", &sound, &level, &city) == 3 && city >= 0 && city < 4) {
                    g_sound = sound != 0;
                    g_level = level;
                    g_city = city;
                }
                show_all(plugin);
            }
            break;
        case AITBASE_EVENT_CHANGED:
            if (strcmp(event->node, "sound") == 0) g_sound = event->code;
            if (strcmp(event->node, "level") == 0) g_level = event->code;
            if (strcmp(event->node, "city") == 0 && event->code >= 0 && event->code < 4) g_city = event->code;
            snprintf(saved, sizeof(saved), "%d,%d,%d", g_sound, g_level, g_city);
            aitbase_storage_set(plugin, "prefs", saved);
            show(plugin);
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
