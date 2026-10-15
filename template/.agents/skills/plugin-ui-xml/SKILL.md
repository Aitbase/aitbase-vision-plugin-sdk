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
| Widgets | `lv_obj`, `lv_label`, `lv_button`, `lv_image` |
| `name` | Required on every widget. Lowercase letters, digits, `_`. Unique within the screen after component prefixing. |
| `x`, `y`, `width`, `height` | Integer pixels (`120` or `120px`). Screen is 240×320. No `%` or `content`. |
| `style_pad_all` | Integer pixels |
| `style_bg_color`, `style_text_color` | Only `#surface`, `#on_surface`, `#accent`, `#control` |
| `lv_label` | `text` (at most 80 bytes); optional `bind_text="title"` (the only subject) |
| `lv_image` | `src` = an image name from `globals.xml` |
| `lv_button` | Exactly one `<event_cb trigger="clicked" callback="aitbase_action" user_data="plugin.increment"/>` |
| Component | `<api><prop name="..." type="string\|int\|color" default="..."/></api>`; use as `<card name="status" title="..."/>`; `$prop` as a whole attribute value |

Limits: 32 nodes, depth 6, 4 images.

## Rejected (do not use)

Other widgets (sliders, lists, text areas, spinners...), `align`, `flex_*`, `grid_*`, flags and states (`hidden`, `clickable`, `focused`), any other `style_*`, `<styles>`, `<consts>`, `<subjects>`, `<fonts>`, `<animations>`, `<translations>`, `bind_text-fmt`, custom colors, DTDs and entities.

## Layout tips

- The device lays out every `<view>` and `lv_obj` as a column: children stack top to bottom in document order. Inside a column, `x` and `y` have no effect. Control spacing with `style_pad_all` and sizes with `width`/`height`.
- Buttons get focus in document order, so put them in the order the user should step through them with LEFT.
- Keep text short. Labels are capped at 80 bytes.

## Validate

Run `aitbase-plugin check --json`. UI errors have codes such as `UI_WIDGET`, `UI_PROPERTY`, `UI_STYLE`, `UI_ACTION` or `UI_BINDING`, and a path like `ui/screens/main.xml/view/lv_button` that shows where the problem is.
