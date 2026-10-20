# Plugin UI XML

Plugin screens are written in XML that follows **LVGL 9.4 XML conventions**:
- one file per screen or component;
- `lv_*` widget tags and `name=`;
- `style_*` attributes and `#constants`;
- `bind_text` and `<event_cb>`.

A bounded subset is accepted (device decision D36). The compiler turns a screen into the `aitbase.plugin-ui` document that the device validates again and renders itself. The device never runs plugin XML through LVGL's XML loader.

> AITBASE VISION is not affiliated with LVGL. This format follows LVGL's conventions so they feel familiar, but it is a restricted subset, and the SDK doesn't claim compatibility with LVGL's tools.

## Package layout

```
ui/
  globals.xml            optional: images used by the screens
  screens/<screen>.xml   one per screen, opened with aitbase_ui_open(p, "<screen>", ...)
  components/<name>.xml  reusable components, used as <name name="..."/>
  assets/                RGB565 images
```

Compile a screen with the CMake helper `aitbase_plugin_add_ui(<target> SCREENS <screen>)`, or by hand:

```bash
python3 tools/plugin_ui_compile.py ui/ <screen> build/<screen>.ui.json
```

## Example

`ui/screens/counter.xml`:

```xml
<screen>
  <view width="240" height="320" style_bg_color="#surface" style_pad_all="8">
    <card name="status" title="Count: 0"/>
    <lv_button name="increment" style_bg_color="#control" style_pad_all="8">
      <lv_label name="caption" text="Increment" style_text_color="#accent"/>
      <event_cb trigger="clicked" callback="aitbase_action" user_data="plugin.increment"/>
    </lv_button>
  </view>
</screen>
```

`ui/components/card.xml`:

```xml
<component>
  <api>
    <prop name="title" type="string" default="Hello"/>
  </api>
  <view style_bg_color="#surface" style_pad_all="8">
    <lv_label name="title" text="$title" style_text_color="#on_surface" bind_text="title"/>
  </view>
</component>
```

The label's full name is `status.title`: the instance name, then the name inside the component. The plugin updates it with `aitbase_ui_set_text(p, "status.title", "title", "Count: 1")`.

## What is accepted

The source of truth is `schema/plugin_ui_registry.json`.

| Element / attribute | Rule |
|---|---|
| `<screen>` | Holds exactly one `<view>`. The view's name is the file name. `<previews>` is ignored. |
| `<component>` | An optional `<api>` with `<prop name type [default]/>` (`type` is `string`, `int` or `color`), and one `<view [extends="lv_obj\|lv_button\|lv_label\|lv_image"]>`. `<previews>` is ignored. |
| Component instance `<card name="…" …/>` | Attributes are `name`, the component's props and `x`/`y`/`width`/`height`. No children. Recursion is rejected. |
| `$prop` | A whole attribute value can be `$prop`. The prop's type must match the attribute. |
| Widgets | `lv_obj`, `lv_label`, `lv_button`, `lv_image`, and the display and input widgets below. |
| `name` | Required on every widget: lowercase letters, digits and `_`. It must be unique after component prefixing. |
| `x`, `y`, `width`, `height` | Integer pixels (`240` or `240px`). `%` and `content` are not accepted yet. |
| Colors (`style_bg_color`, `style_text_color`, `style_border_color`, `style_line_color`) | Any `#rrggbb` or `0xRRGGBB`, or a platform token: `#surface`, `#on_surface`, `#accent`, `#control`. |
| `style_pad_all` | Integer pixels, 0–320. |
| `style_pad_top`, `_bottom`, `_left`, `_right`, `_row`, `_column` | 0–64. |
| `style_radius` | 0–40. |
| `style_border_width` | 0–8. |
| `style_bg_opa`, `style_opa` | 0–255. |
| `style_text_font` | The device's font sizes: `14`, `16`, `20`, `24` or `28` (Latin and Vietnamese). |
| `style_text_align` | `left`, `center` or `right`. |
| `lv_obj` layout | `flex_flow` (`row`, `column`, `row_wrap`, `column_wrap`); `style_flex_main_place` and `style_flex_track_place` (`start`, `center`, `end`, `space_between`, `space_around`, `space_evenly`); `style_flex_cross_place` (`start`, `center`, `end`). |
| `lv_label` | `text` (at most 80 bytes); `bind_text` (a platform subject: `title` or `value.1` to `value.16` on a screen, `face.1` to `face.16` on an idle face). |
| `lv_image` | `src` is the name of an image declared in `globals.xml`: `<images><file name="logo" src_path="assets/logo.rgb565"/></images>`. |
| `lv_button` | Must contain exactly one `<event_cb trigger="clicked" callback="aitbase_action" user_data="<action>"/>`, where `<action>` is a registered action (`plugin.increment`). |

### Display widgets

They show values; they never take focus or input. Bound values are text, set with `aitbase_ui_set_text` on a screen or `aitbase_face_set` on a face. A gauge or a led takes the leading integer of the text (`"45%"` is 45, `"N/A"` is 0), so one value can feed a label and a gauge.

| Widget | Attributes | Binding | Bounds |
|---|---|---|---|
| `lv_bar` | `value`, `min_value`, `max_value`, `style_bg_color-indicator` | `bind_value` | -1000..1000; min below max |
| `lv_arc` | `value`, `min_value`, `max_value`, `rotation`, `bg_start_angle`, `bg_end_angle`, `style_arc_width`, `style_arc_color-indicator` | `bind_value` | angles 0–360; width 2–16; no knob |
| `lv_led` | `brightness`, `color` | `bind_value` (the brightness) | 0–255 |
| `lv_spinner` | `anim_duration`, `arc_length`, `style_arc_color-indicator` | — | 500–5000 ms; at most 2 per screen |
| `lv_line` | `points="x,y x,y …"`, `style_line_color` | — | 2 to 16 points, each within ±320 |
| `lv_scale` | `mode` (`horizontal_top`, `horizontal_bottom`, `vertical_left`, `vertical_right`, `round_inner`, `round_outer`), `min_value`, `max_value`, `total_tick_count`, `major_tick_every`, `style_line_color-indicator` | — | ≤ 41 ticks |
| `lv_chart` | `type` (`line`, `bar`), `point_count`, `min_value`, `max_value`, `style_line_color-indicator`; up to two `<lv_chart-series [values="1 2 3"] [bind_values="value.N"]/>` children | `bind_values` per series: comma-separated integers | ≤ 32 points |
| `lv_table` | `row_count`, `column_count`; `<lv_table-cell row column value/>` and `<lv_table-row row bind_value/>` children | a row: its cells separated by `\|` | ≤ 8 rows × 4 columns; cells ≤ 32 bytes |

Indicator colors use LVGL's part names with a hyphen (`style_bg_color-indicator`), because XML can't take `:` in an attribute name.

### Input widgets (screens only)

The user changes them with the device's two buttons (see [Focus and buttons](#focus-and-buttons)); the plugin hears each change as `AITBASE_EVENT_CHANGED`, with `event->node` the widget's name and `event->code` its value. Set a value from the plugin with `aitbase_ui_set_text` on the widget's binding; that sends no `CHANGED`. They're never allowed on an idle face.

| Widget | Attributes | Value (`event->code`, and the binding) | Bounds |
|---|---|---|---|
| `lv_checkbox` | `text`, `checked` (`true` or `false`), `style_bg_color-indicator` | `bind_checked`: 0 or 1 | text ≤ 80 bytes |
| `lv_switch` | `checked`, `style_bg_color-indicator` | `bind_checked`: 0 or 1 | — |
| `lv_slider` | `value`, `min_value`, `max_value`, `step`, `style_bg_color-indicator` | `bind_value`: the value | -1000..1000; step 1–1000 |
| `lv_roller` | `options` (separated by `\n`), `selected` | `bind_value`: the option's index from 0 | 1 to 8 options of 1 to 24 bytes |
| `lv_dropdown` | `options`, `selected` | `bind_value`: the option's index | the same |

`step` is AITBASE's: how far one button press moves a slider. A bound value is text, as for gauges: `"1"` checks a checkbox, `"40"` sets a slider.

The platform constants and subjects are listed in `schema/aitbase_globals.xml`. Plugins can't define their own fonts or subjects.

**Idle faces.** A screen named in `plugin.json`'s `idle_face` may be shown on the device's home screen instead of the clock, when the owner chooses it. It must be display only: no `lv_button`, no input widget and no `<event_cb>`, widgets bound only to `face.1` to `face.16` (set with `aitbase_face_set`), at most 40 widgets and 12 KiB compiled. Display widgets are fine on a face. The device draws its "Plugin" badge in the top right corner, over the face. `aitbase-plugin check` reports `IDLE_FACE_BOUNDS` otherwise.

Images are RGB565: `uint16 width`, `uint16 height`, then pixels. They must be at most 32×32 and 8 KB.

The registry's `limits` cap nodes (64 per screen), nesting depth, text length and total size (24 KiB compiled). Devices older than D36 refuse the new widgets and styles; `check` can't tell which firmware a device runs.

## What is rejected

Everything not listed above is rejected with a stable error code (`UI_WIDGET`, `UI_PROPERTY`, `UI_STYLE`, `UI_EVENT`, `UI_ACTION`, `UI_BINDING`…). Nothing is silently ignored. In particular:
- other widgets: `lv_canvas`, `lv_textarea`, `lv_keyboard`, message boxes, tab views, windows, menus, calendars and span groups among them;
- flags and states (`hidden`, `clickable`, `focused`…);
- `align`, `grid_*`, and flex properties other than those above;
- other `style_*` properties, and styles on parts other than the indicators above;
- fonts other than the device's sizes, and images over 32×32;
- `<styles>`, `<consts>`, `<subjects>`, `<fonts>`, `<gradients>`, `<animations>` and `<translations>`;
- `screen_load_event` and the `subject_*_event` elements;
- `bind_text-fmt`;
- DTDs and entities.

The registry will grow; each addition is a new, validated entry.

## Focus and buttons

The device owns focus. LEFT moves focus between buttons and input widgets in document order, RIGHT activates the focused one, and a LEFT long press always leaves the screen. Plugins can't style or hide the focus outline.

- **A button** sends its action (the `clicked` trigger).
- **A checkbox or switch** toggles, and the plugin gets `AITBASE_EVENT_CHANGED`.
- **A slider, roller or dropdown** enters edit mode; the focus outline turns amber.
  - In edit mode, LEFT press is one step down (the previous option) and RIGHT press one step up (the next option). Each change is an `AITBASE_EVENT_CHANGED`.
  - A RIGHT long press confirms and leaves edit mode. A long press always comes after a press of the same button, so the device takes back that press's step first.
  - A LEFT long press still leaves the screen.
  - No `AITBASE_EVENT_KEY` arrives in edit mode, so a press is never both an edit and a key.
- **`AITBASE_EVENT_CHANGED` is limited** to 10 a second per screen. Between two, the latest value of each widget waits, so holding a button never floods the plugin. A waiting value is sent before the screen closes.

The plugin also receives the buttons as `AITBASE_EVENT_KEY` while its screen shows, outside edit mode: LEFT press, RIGHT press and RIGHT long press, each before the focus or `clicked` action of the same press. A screen without buttons or inputs is driven by these events alone. A LEFT long press is never delivered.
