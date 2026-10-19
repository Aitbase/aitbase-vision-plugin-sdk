# Plugin UI XML

Plugin screens are written in XML that follows **LVGL 9.4 XML conventions**:
- one file per screen or component;
- `lv_*` widget tags and `name=`;
- `style_*` attributes and `#constants`;
- `bind_text` and `<event_cb>`.

Only a small, safe subset is accepted. The compiler turns a screen into the `aitbase.plugin-ui` document that the device validates again and renders itself. The device never runs plugin XML through LVGL's XML loader.

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
| Widgets | `lv_obj`, `lv_label`, `lv_button` and `lv_image`. |
| `name` | Required on every widget: lowercase letters, digits and `_`. It must be unique after component prefixing. |
| `x`, `y`, `width`, `height` | Integer pixels (`240` or `240px`). `%` and `content` are not accepted yet. |
| `style_pad_all` | Integer pixels. |
| `style_bg_color`, `style_text_color` | Platform color tokens only: `#surface`, `#on_surface`, `#accent`, `#control`, or their exact `0xRRGGBB` values. |
| `lv_label` | `text` (at most 80 bytes); `bind_text` (a platform subject: `title`, or `face.1` to `face.16` for an idle face). |
| `lv_image` | `src` is the name of an image declared in `globals.xml`: `<images><file name="logo" src_path="assets/logo.rgb565"/></images>`. |
| `lv_button` | Must contain exactly one `<event_cb trigger="clicked" callback="aitbase_action" user_data="<action>"/>`, where `<action>` is a registered action (`plugin.increment`). |

The platform constants and subjects are listed in `schema/aitbase_globals.xml`. Plugins can't define their own colors, fonts or subjects.

**Idle faces.** A screen named in `plugin.json`'s `idle_face` may be shown on the device's home screen instead of the clock, when the owner chooses it. It must be display only: no `lv_button` and no `<event_cb>`, labels bound only to `face.1` to `face.16` (set with `aitbase_face_set`), at most 24 widgets and 8 KiB compiled. The device draws its "Plugin" badge in the top right corner, over the face. `aitbase-plugin check` reports `IDLE_FACE_BOUNDS` otherwise.

Images are RGB565: `uint16 width`, `uint16 height`, then pixels. They must be at most 32×32 and 8 KB.

The registry's `limits` cap nodes, nesting depth, text length and total size.

## What is rejected

Everything not listed above is rejected with a stable error code (`UI_WIDGET`, `UI_PROPERTY`, `UI_STYLE`, `UI_EVENT`, `UI_ACTION`, `UI_BINDING`…). Nothing is silently ignored. In particular:
- other widgets;
- flags and states (`hidden`, `clickable`, `focused`…);
- `align`, `flex_*` and `grid_*`;
- other `style_*` properties;
- `<styles>`, `<consts>`, `<subjects>`, `<fonts>`, `<gradients>`, `<animations>` and `<translations>`;
- `screen_load_event` and the `subject_*_event` elements;
- `bind_text-fmt`;
- DTDs and entities.

The registry will grow; each addition is a new, validated entry.

## Focus and buttons

The device owns focus. LEFT moves focus between buttons in document order, RIGHT activates the focused button (the `clicked` trigger), and a LEFT long press always leaves the screen. Plugins can't style or hide the focus outline.

The plugin also receives the buttons as `AITBASE_EVENT_KEY` while its screen shows: LEFT press, RIGHT press and RIGHT long press, each before the focus or `clicked` action of the same press. A screen without buttons is driven by these events alone. A LEFT long press is never delivered.
