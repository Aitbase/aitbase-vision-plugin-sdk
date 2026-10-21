---
name: plugin-ui-xml
description: Write AITBASE VISION plugin screens and components in the restricted LVGL-style XML so they pass validation. Use whenever editing files under ui/.
---

# Plugin UI XML

The format follows LVGL 9.4 XML conventions, but only a small subset is accepted. Anything not listed here is rejected; nothing is silently ignored. The source of truth is `sdk/schema/plugin_ui_registry.json`, and the full reference is `sdk/docs/ui-xml.md`.

## Files

```
ui/screens/<screen>.xml     <screen> with exactly one <view>
ui/components/<name>.xml    <component> with optional <api> and one <view>
ui/globals.xml              optional: <globals><images><file name="logo" src_path="assets/logo.rgb565"/></images></globals>
ui/assets/                  RGB565 images: uint16 width, uint16 height, then pixels; at most 32×32 and 8 KB
```

## Allowed

| What | Rule |
|---|---|
| Widgets | `lv_obj`, `lv_label`, `lv_button`, `lv_image`; display widgets `lv_bar`, `lv_arc`, `lv_led`, `lv_spinner` (≤ 2), `lv_line`, `lv_scale`, `lv_chart`, `lv_table`; input widgets `lv_checkbox`, `lv_switch`, `lv_slider`, `lv_roller`, `lv_dropdown` on screens, never a face (attributes in `sdk/docs/ui-xml.md`) |
| `name` | Required on every widget. Lowercase letters, digits, `_`. Unique within the screen after component prefixing. |
| `x`, `y`, `width`, `height` | Integer pixels (`120` or `120px`). Screen is 240×320. No `%` or `content`. |
| `style_pad_all` | Integer pixels; `style_pad_top/_bottom/_left/_right/_row/_column` 0–64 |
| Colors: `style_bg_color`, `style_text_color`, `style_border_color`, `style_line_color` | `#rrggbb`, `0xRRGGBB`, or `#surface`, `#on_surface`, `#accent`, `#control` |
| Other styles | `style_radius` 0–40, `style_border_width` 0–8, `style_bg_opa` and `style_opa` 0–255, `style_text_font` 14/16/20/24/28, `style_text_align` left/center/right |
| `lv_obj` layout | `flex_flow` (row, column, row_wrap, column_wrap) and `style_flex_main_place`, `style_flex_cross_place`, `style_flex_track_place` |
| Placement (any widget) | `flex_grow` 0–8; `floating="true"` with `align` (top_left, top_mid, top_right, left_mid, center, right_mid, bottom_left, bottom_mid, bottom_right) to sit over a sibling, `x`/`y` as the offset |
| `lv_label` | `text` (at most 80 bytes); `long_mode` wrap/scroll/scroll_circular/dots/clip; optional `bind_text="title"` or `value.1`–`value.16` (`face.1`–`face.16` on an idle face) |
| Gauges | `lv_arc` and `lv_spinner` take `style_arc_color` for the track. `lv_bar`, `lv_arc` and `lv_led` take `bind_value="value.N"`; the bound text's leading integer is the value (`"45%"` is 45) |
| Inputs | `lv_checkbox text checked="true\|false" bind_checked`, `lv_switch checked bind_checked`, `lv_slider value min_value max_value step bind_value`, `lv_roller`/`lv_dropdown options="A\\nB\\nC" selected bind_value` (1–8 options of ≤ 24 bytes). Each change is `AITBASE_EVENT_CHANGED` (`ev->node`, value in `ev->code`) |
| `lv_image` | `src` = an image name from `globals.xml` |
| `lv_button` | Exactly one `<event_cb trigger="clicked" callback="aitbase_action" user_data="plugin.increment"/>` |
| Component | `<api><prop name="..." type="string\|int\|color" default="..."/></api>`; use as `<card name="status" title="..."/>`; `$prop` as a whole attribute value |

Limits: 64 nodes (40 on an idle face), depth 6, 4 images, 24 KiB compiled (12 KiB for a face). Needs device firmware with D36 for the display and input widgets and the new styles, and firmware with SDK 0.13.0's attributes for `long_mode`, `flex_grow`, `floating`, `align` and `style_arc_color`.

## Rejected (do not use)

Other widgets (lists, text areas, keyboards, canvases, tab views...), `grid_*`, other `flex_*`, flags and states other than `floating` (`hidden`, `clickable`, `focused`), any other `style_*`, `<styles>`, `<consts>`, `<subjects>`, `<fonts>`, `<animations>`, `<translations>`, `bind_text-fmt`, DTDs and entities.

## Layout tips

- The device lays out every `<view>` and `lv_obj` as a column unless `flex_flow` says otherwise: children follow each other in document order. Inside a flex layout, `x` and `y` have no effect, except on a `floating="true"` widget, where they offset its `align`. Control spacing with `style_pad_*` and sizes with `width`/`height`.
- Buttons get focus in document order, so put them in the order the user should step through them with LEFT.
- Keep text short. Labels are capped at 80 bytes.

## Validate

Run `aitbase-plugin check --json`. UI errors have codes such as `UI_WIDGET`, `UI_PROPERTY`, `UI_STYLE`, `UI_ACTION` or `UI_BINDING`, and a path like `ui/screens/main.xml/view/lv_button` that shows where the problem is.
