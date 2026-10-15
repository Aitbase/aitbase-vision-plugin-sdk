# Plugin UI XML (v1, experimental)

Plugin screens are written in XML and compiled to the `aitbase.plugin-ui` JSON document that the device loads:

```bash
python3 tools/plugin_ui_compile.py ui/ui.xml ui/ build/ui.json
```

In CMake, use `aitbase_plugin_add_ui()` from `cmake/AitbasePluginUi.cmake`.

The device validates the compiled document again before rendering anything. It never runs plugin XML through LVGL's own XML loader.

## What is allowed

`schema/plugin_ui_registry.json` defines what is allowed. Both the compiler and the device enforce it:

- **Widgets:** `container`, `label`, `button` and `image`.
- **Integer properties:** `x`, `y`, `width`, `height` and `padding`.
- **Colors:** `background` and `color`, using only the listed color tokens.
- **Events and actions:**
  - every `button` must declare `<on event="activate" action="..."/>` with a registered action;
  - focus moves between buttons in document order.
- **Bindings:** `<bind property="text" key="title" type="string"/>` on a `label`. The plugin sets bound values with `aitbase_ui_set_text()`.
- **Components:**
  - define one with `<component id="...">` under `<components>`, and reuse it with `<use component="..." id="..."/>`;
  - node IDs inside a component get the `use` id as a prefix (for example `status.title`).
- **Images:** RGB565 assets under `assets/`, in the format `uint16 width`, `uint16 height`, then pixels. They must be at most 32×32 and 8 KB.
- **Limits:** see the `limits` section of the registry (nodes, depth, text length, document size).

The registry is small on purpose, and it will grow. The compiler rejects anything it doesn't list, with a stable error code (`UI_WIDGET`, `UI_PROPERTY`, `UI_ACTION`…).

## Example

```xml
<plugin-ui version="1">
  <components>
    <component id="card">
      <container id="root" background="#101820" padding="8">
        <label id="title" text="Plugin demo" color="#ffffff">
          <bind property="text" key="title" type="string"/>
        </label>
      </container>
    </component>
  </components>
  <screen id="demo">
    <container id="main" width="240" height="320" background="#101820" padding="8">
      <use component="card" id="status"/>
      <button id="increment" background="#334455" padding="8">
        <label id="caption" text="Increment" color="#00e5ff"/>
        <on event="activate" action="plugin.increment"/>
      </button>
    </container>
  </screen>
</plugin-ui>
```
