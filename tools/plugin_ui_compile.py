#!/usr/bin/env python3
"""Compile plugin UI XML to the aitbase.plugin-ui document the device validates and renders.

Plugin UI follows LVGL 9.4 XML conventions (one file per screen or component, lv_* widgets,
name=, style_* attributes, #constants, bind_text, <event_cb>), restricted to what
schema/plugin_ui_registry.json allows. Anything else is rejected with a stable error code;
nothing is silently ignored. The output is a neutral, versioned format owned by AITBASE.
"""

from __future__ import annotations

import argparse
import json
import re
import struct
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

# Resolve the sibling registry module even under `python3 -I`, which drops the script directory.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from plugin_ui_registry import load_registry  # noqa: E402


REGISTRY = load_registry()
LIMITS = REGISTRY["limits"]
FORMAT = REGISTRY["format"]
FORMAT_VERSION = REGISTRY["version"]
MAX_XML_BYTES = LIMITS["max_representation_bytes"]
MAX_NODES = LIMITS["max_nodes"]
MAX_DEPTH = LIMITS["max_depth"]
MAX_TEXT = LIMITS["max_text"]
MAX_ASSET_BYTES = LIMITS["max_asset_bytes"]
MAX_ASSETS = LIMITS["max_assets"]
MAX_IMAGE_DIMENSION = LIMITS["max_image_dimension"]
WIDGETS = REGISTRY["widgets"]
INT_PROPS = {name: tuple(bounds) for name, bounds in REGISTRY["int_props"].items()}
COLOR_PROPS = set(REGISTRY["color_props"])
ATTRS = {"id"} | set(INT_PROPS) | COLOR_PROPS
STYLE_COLORS = set(REGISTRY["color_tokens"])
EVENTS = set(REGISTRY["events"])
ACTION_IDS = set(REGISTRY["actions"])
SHARED_PROPS = REGISTRY["shared_props"]
WIDGET_PROPS = REGISTRY["widget_props"]
BINDING_KEYS = REGISTRY["binding_keys"]
BINDING_TYPE = REGISTRY["binding_type"]
BINDABLE = REGISTRY["bindable"]
ID_RE = re.compile(r"^[a-z][a-z0-9_.-]{0,%d}$" % (LIMITS["max_id"] - 1))


def _binding(widget: str, prop: str, key: str, attr: str, path: str) -> dict:
    """A binding of widget.prop to a platform subject (title, face.N, value.N)."""
    if prop not in BINDABLE.get(widget, ()):
        _fail("UI_BINDING", path, f"{attr}: {widget} has no bindable {prop}")
    if key not in BINDING_KEYS:
        _fail("UI_BINDING", path, f"{attr} subject {key} is not a platform subject")
    return {"property": prop, "key": key, "type": BINDING_TYPE}


class UIError(ValueError):
    def __init__(self, code: str, path: str, detail: str):
        self.code, self.path, self.detail = code, path, detail
        super().__init__(f"{code} at {path}: {detail}")


def _fail(code: str, path: str, detail: str):
    raise UIError(code, path, detail)


def _asset(root: Path, source: str, path: str) -> dict:
    if not source or "\\" in source or source.startswith("/") or ":" in source:
        _fail("UI_ASSET_PATH", path, "asset path must be package relative")
    try:
        package = root.resolve(strict=True)
    except OSError:
        _fail("UI_ASSET_ROOT", path, "package asset directory is unavailable")
    candidate = package.joinpath(source)
    if any(part in ("", ".", "..") for part in Path(source).parts):
        _fail("UI_ASSET_PATH", path, "asset path contains an unsafe segment")
    cursor = package
    for part in Path(source).parts:
        cursor = cursor / part
        if cursor.is_symlink():
            _fail("UI_ASSET_PATH", path, "asset paths cannot contain symlinks")
    try:
        resolved = candidate.resolve(strict=True)
        resolved.relative_to(package)
    except (OSError, ValueError):
        _fail("UI_ASSET_MISSING", path, "asset is missing or escapes the package")
    if not resolved.is_file():
        _fail("UI_ASSET_PATH", path, "asset must be a regular package file")
    if resolved.stat().st_size > MAX_ASSET_BYTES:
        _fail("UI_ASSET_SIZE", path, f"asset exceeds {MAX_ASSET_BYTES} bytes")
    data = resolved.read_bytes()
    if len(data) > MAX_ASSET_BYTES:
        _fail("UI_ASSET_SIZE", path, f"asset exceeds {MAX_ASSET_BYTES} bytes")
    if resolved.suffix != ".rgb565" or len(data) < 4:
        _fail("UI_ASSET_FORMAT", path, "expected a packed RGB565 asset")
    width, height = struct.unpack_from("<HH", data)
    if not width or not height or width > MAX_IMAGE_DIMENSION or height > MAX_IMAGE_DIMENSION:
        _fail("UI_ASSET_DIMENSIONS", path, "RGB565 dimensions are outside the PoC profile")
    if len(data) != 4 + width * height * 2:
        _fail("UI_ASSET_SIZE", path, "RGB565 payload length does not match dimensions")
    return {"path": source, "format": "rgb565", "width": width, "height": height,
            "bytes": len(data), "data_hex": data[4:].hex()}


# LVGL 9.4 widget tags accepted by the compiler, mapped to the neutral registry widget names (D36).
LVGL_WIDGETS = {"lv_obj": "container", "lv_label": "label", "lv_button": "button", "lv_image": "image",
                "lv_bar": "bar", "lv_arc": "arc", "lv_led": "led", "lv_spinner": "spinner", "lv_line": "line",
                "lv_scale": "scale", "lv_chart": "chart", "lv_table": "table"}
# LVGL style attributes mapped to registry properties.
STYLE_INT = {"style_pad_all": "padding", "style_pad_top": "pad_top", "style_pad_bottom": "pad_bottom",
             "style_pad_left": "pad_left", "style_pad_right": "pad_right", "style_pad_row": "pad_row",
             "style_pad_column": "pad_column", "style_radius": "radius", "style_border_width": "border_width",
             "style_bg_opa": "bg_opa", "style_opa": "opa"}
STYLE_COLOR = {"style_bg_color": "background", "style_text_color": "color", "style_border_color": "border_color",
               "style_line_color": "color"}
STYLE_TYPED = {"style_text_font": "font", "style_text_align": "text_align"}
STYLE_ATTRS = {**STYLE_INT, **STYLE_COLOR, **STYLE_TYPED}
# The indicator part's color: the filled part of a bar or arc, a spinner's arc, a scale's major ticks, a
# chart's first series. A led's color is its own attribute, as in LVGL.
INDICATOR_ATTRS = {"bar": "style_bg_color-indicator", "arc": "style_arc_color-indicator",
                   "spinner": "style_arc_color-indicator", "scale": "style_line_color-indicator",
                   "chart": "style_line_color-indicator", "led": "color"}
# Flex layout on containers: LVGL's flex_flow attribute and its style_flex_*_place styles.
CONTAINER_ATTRS = {"flex_flow": "flex_flow", "style_flex_main_place": "flex_main_place",
                   "style_flex_cross_place": "flex_cross_place", "style_flex_track_place": "flex_track_place"}
# Widget attributes (LVGL 9.4 XML names) mapped to registry properties.
WIDGET_ATTRS = {
    "bar": {"value": "value", "min_value": "min", "max_value": "max"},
    "arc": {"value": "value", "min_value": "min", "max_value": "max", "rotation": "rotation",
            "bg_start_angle": "bg_start_angle", "bg_end_angle": "bg_end_angle", "style_arc_width": "thickness"},
    "led": {"brightness": "brightness"},
    "spinner": {"anim_duration": "duration", "arc_length": "arc_length"},
    "line": {"points": "points"},
    "scale": {"mode": "scale_mode", "min_value": "min", "max_value": "max", "total_tick_count": "total_ticks",
              "major_tick_every": "major_every"},
    "chart": {"type": "chart_type", "point_count": "point_count", "min_value": "min", "max_value": "max"},
    "table": {"row_count": "rows", "column_count": "cols"},
}
LAYOUT_ATTRS = ("x", "y", "width", "height")
TOKEN_NAMES = REGISTRY["color_token_names"]
TOKEN_BY_VALUE = {value: name for name, value in TOKEN_NAMES.items()}
# <event_cb callback> value that marks a registry action; user_data carries the action id.
ACTION_CALLBACK = "aitbase_action"
# LVGL event trigger accepted for buttons; the device fires it on RIGHT-press activation.
ACTION_TRIGGER = "clicked"
NAME_RE = re.compile(r"^[a-z][a-z0-9_]{0,%d}$" % (LIMITS["max_id"] - 1))
PROP_TYPES = {"string", "int", "color"}


def _read_xml(path: Path, label: str, budget: list) -> ET.Element:
    try:
        data = path.read_bytes()
    except OSError:
        _fail("UI_FILE_MISSING", label, "file is missing or unreadable")
    budget[0] += len(data)
    if budget[0] > MAX_XML_BYTES:
        _fail("UI_XML_SIZE", label, f"UI files exceed {MAX_XML_BYTES} bytes in total")
    try:
        data.decode("utf-8")
    except UnicodeDecodeError:
        _fail("UI_XML_ENCODING", label, "only UTF-8 XML is supported")
    # Reject DTDs and entities before the host parser sees them.
    if re.search(br"<!\s*(DOCTYPE|ENTITY)\b", data, re.IGNORECASE):
        _fail("UI_XML_DTD", label, "DTD and entity declarations are not supported")
    try:
        return ET.fromstring(data)
    except ET.ParseError as exc:
        line, col = exc.position
        _fail("UI_XML_SYNTAX", f"{label}:{line}:{col}", str(exc))


def _int_value(raw: str, key: str, path: str) -> int:
    text = raw[:-2] if raw.endswith("px") else raw
    try:
        value = int(text)
    except ValueError:
        _fail("UI_PROPERTY_VALUE", path, f"{key} must be an integer number of pixels")
    low, high = INT_PROPS[key]
    if not low <= value <= high:
        _fail("UI_PROPERTY_VALUE", path, f"{key} must be within [{low}, {high}]")
    return value


HEX_COLOR = re.compile(r"^(#|0x)([0-9a-fA-F]{6})$")


def _color_value(raw: str, key: str, path: str) -> str:
    """#rrggbb or 0xRRGGBB (any color since D36), or a platform token such as #accent."""
    literal = HEX_COLOR.fullmatch(raw)
    if literal:
        return "#" + literal.group(2).lower()
    if raw.startswith("#") and raw[1:] in TOKEN_NAMES:
        return TOKEN_NAMES[raw[1:]]
    _fail("UI_STYLE", path, f"{key}: expected #rrggbb, 0xRRGGBB or a token ("
          + ", ".join("#" + name for name in TOKEN_NAMES) + ")")


def _typed_value(raw: str, prop: str, key: str, path: str):
    """A registry property's value from its XML text: ints, sets of ints, enums and integer lists."""
    spec = WIDGET_PROPS.get(prop) or SHARED_PROPS[prop]
    kind = spec["type"]
    if kind in ("int", "int_set"):
        try:
            value = int(raw[:-2] if raw.endswith("px") else raw)
        except ValueError:
            _fail("UI_PROPERTY_VALUE", path, f"{key} must be an integer")
        if kind == "int" and not spec["min"] <= value <= spec["max"]:
            _fail("UI_PROPERTY_VALUE", path, f"{key} must be within [{spec['min']}, {spec['max']}]")
        if kind == "int_set" and value not in spec["values"]:
            _fail("UI_PROPERTY_VALUE", path, f"{key} must be one of {spec['values']}")
        return value
    if kind == "enum":
        if raw not in spec["values"]:
            _fail("UI_PROPERTY_VALUE", path, f"{key} must be one of {', '.join(spec['values'])}")
        return raw
    if kind == "int_list":
        try:
            values = [int(part) for part in re.split(r"[\s,]+", raw.strip()) if part]
        except ValueError:
            _fail("UI_PROPERTY_VALUE", path, f"{key} must be integers")
        if len(values) > spec["max_items"] or any(not spec["min"] <= v <= spec["max"] for v in values):
            _fail("UI_PROPERTY_VALUE", path,
                  f"{key}: at most {spec['max_items']} values within [{spec['min']}, {spec['max']}]")
        return values
    _fail("UI_PROPERTY", path, f"{key} is not supported here")


def _check_combination(widget: str, props: dict, path: str) -> None:
    """The rules between a widget's properties, as the device checks them (D36)."""
    if ("min" in props or "max" in props) and props.get("min", 0) >= props.get("max", 100):
        _fail("UI_PROPERTY_VALUE", path, "min_value must be below max_value")
    if widget == "line" and "points" in props and (len(props["points"]) < 4 or len(props["points"]) % 2):
        _fail("UI_PROPERTY_VALUE", path, "points are x,y pairs, at least two of them")
    if widget == "chart":
        points = props.get("point_count", 10)
        if any(len(props.get(s, [])) > points for s in ("series_1", "series_2")):
            _fail("UI_PROPERTY_VALUE", path, f"a series has more values than point_count ({points})")
    if widget == "table" and len(props.get("cells", [])) > props.get("rows", 1) * props.get("cols", 1):
        _fail("UI_PROPERTY_VALUE", path, "cells outside row_count x column_count")
    if widget == "scale" and props.get("major_every", 5) > props.get("total_ticks", 11):
        _fail("UI_PROPERTY_VALUE", path, "major_tick_every can't exceed total_tick_count")


def _load_images(ui_dir: Path, budget: list) -> dict:
    path = ui_dir / "globals.xml"
    if not path.exists():
        return {}
    root = _read_xml(path, "globals.xml", budget)
    if root.tag != "globals" or root.attrib:
        _fail("UI_SCHEMA", "globals.xml", "expected a <globals> root without attributes")
    images = {}
    for section in root:
        where = f"globals.xml/{section.tag}"
        if section.tag != "images" or section.attrib:
            _fail("UI_SCHEMA", where, "plugin globals may only declare <images>; colors and "
                  "subjects come from the platform globals")
        for entry in section:
            entry_path = f"{where}/{entry.tag}"
            if entry.tag != "file" or set(entry.attrib) != {"name", "src_path"} or len(entry):
                _fail("UI_ASSET", entry_path, "expected <file name=... src_path=.../>")
            name = entry.get("name")
            if not NAME_RE.fullmatch(name):
                _fail("UI_NODE_ID", entry_path, f"invalid image name {name}")
            if name in images:
                _fail("UI_DUPLICATE_ID", entry_path, f"duplicate image {name}")
            images[name] = entry.get("src_path")
    return images


def compile_package(ui_dir: Path, screen: str) -> dict:
    """Compiles ui_dir/screens/<screen>.xml (with components/ and globals.xml) to the device format."""
    if not NAME_RE.fullmatch(screen or ""):
        _fail("UI_SCREEN", "/", f"invalid screen name {screen!r}")
    budget = [0]
    images = _load_images(ui_dir, budget)
    components = {}

    def component(tag: str, path: str):
        if tag not in components:
            if not NAME_RE.fullmatch(tag):
                _fail("UI_WIDGET", path, f"unknown widget {tag}")
            file = ui_dir / "components" / f"{tag}.xml"
            if not file.is_file():
                _fail("UI_WIDGET", path, f"unknown widget or component {tag}")
            label = f"components/{tag}.xml"
            root = _read_xml(file, label, budget)
            if root.tag != "component" or root.attrib:
                _fail("UI_COMPONENT", label, "expected a <component> root without attributes")
            props, view = {}, None
            for section in root:
                if section.tag == "previews":
                    continue  # LVGL Editor metadata
                if section.tag == "api" and not section.attrib and not props:
                    for prop in section:
                        prop_path = f"{label}/api/{prop.tag}"
                        if prop.tag != "prop" or not {"name", "type"} <= set(prop.attrib) <= {"name", "type", "default"} or len(prop):
                            _fail("UI_COMPONENT", prop_path, "expected <prop name type [default]/>")
                        if prop.get("type") not in PROP_TYPES or not NAME_RE.fullmatch(prop.get("name")):
                            _fail("UI_COMPONENT", prop_path, "prop type must be string, int or color")
                        props[prop.get("name")] = (prop.get("type"), prop.get("default"))
                    continue
                if section.tag == "view" and view is None:
                    view = section
                    continue
                _fail("UI_COMPONENT", f"{label}/{section.tag}", "component supports one <api> and one <view>")
            if view is None:
                _fail("UI_COMPONENT", label, "component requires a <view>")
            components[tag] = (props, view, label)
        return components[tag]

    ids, assets, count = set(), {}, 0
    per_widget = {}  # widgets with max_per_document (spinners)

    def resolve(raw: str, args: dict, path: str, key: str, kind: str) -> str:
        if not raw.startswith("$"):
            return raw
        name = raw[1:]
        if name not in args:
            _fail("UI_COMPONENT_REF", path, f"{key} references undeclared prop ${name}")
        prop_type, value = args[name]
        if prop_type != kind:
            _fail("UI_COMPONENT_REF", path, f"{key} expects a {kind}, but ${name} is a {prop_type}")
        return value

    def build(tag: str, attrib: dict, children: list, node_id: str, scope: str, args: dict,
              depth: int, path: str, ancestry: tuple) -> dict:
        """Builds one widget node. Child names are prefixed with scope (the enclosing instance)."""
        nonlocal count
        if depth > MAX_DEPTH:
            _fail("UI_DEPTH", path, f"nesting exceeds {MAX_DEPTH}")
        widget = LVGL_WIDGETS[tag]
        count += 1
        if count > MAX_NODES:
            _fail("UI_NODE_LIMIT", path, f"expanded widget count exceeds {MAX_NODES}")
        if node_id in ids:
            _fail("UI_DUPLICATE_ID", path, f"duplicate node name {node_id}")
        ids.add(node_id)
        spec = WIDGETS[widget]
        if spec.get("max_per_document"):
            per_widget[widget] = per_widget.get(widget, 0) + 1
            if per_widget[widget] > spec["max_per_document"]:
                _fail("UI_WIDGET_LIMIT", path, f"at most {spec['max_per_document']} {tag} per screen")
        widget_attrs = WIDGET_ATTRS.get(widget, {})
        allowed = set(LAYOUT_ATTRS) | set(STYLE_ATTRS) | set(widget_attrs)
        if widget == "label":
            allowed |= {"text", "bind_text"}
        if widget == "image":
            allowed |= {"src"}
        if widget == "container":
            allowed |= set(CONTAINER_ATTRS)
        if widget in INDICATOR_ATTRS:
            allowed.add(INDICATOR_ATTRS[widget])
        if widget in ("bar", "arc", "led"):
            allowed.add("bind_value")
        unknown = sorted(set(attrib) - allowed)
        if unknown:
            _fail("UI_PROPERTY", path, f"unsupported attribute {unknown[0]}")
        props = {}

        def put(prop, value, key):
            if prop in props:
                _fail("UI_PROPERTY", path, f"{key} sets {prop} twice")
            props[prop] = value

        for key in LAYOUT_ATTRS:
            if key in attrib:
                props[key] = _int_value(resolve(attrib[key], args, path, key, "int"), key, path)
        for key, prop in STYLE_INT.items():
            if key in attrib:
                put(prop, _int_value(resolve(attrib[key], args, path, key, "int"), prop, path), key)
        for key, prop in STYLE_COLOR.items():
            if key in attrib:
                put(prop, _color_value(resolve(attrib[key], args, path, key, "color"), key, path), key)
        if widget in INDICATOR_ATTRS and INDICATOR_ATTRS[widget] in attrib:
            key = INDICATOR_ATTRS[widget]
            put("indicator_color", _color_value(resolve(attrib[key], args, path, key, "color"), key, path), key)
        for key, prop in STYLE_TYPED.items():
            if key in attrib:
                kind = "int" if SHARED_PROPS[prop]["type"] == "int_set" else "string"
                put(prop, _typed_value(resolve(attrib[key], args, path, key, kind), prop, key, path), key)
        typed = {**widget_attrs, **(CONTAINER_ATTRS if widget == "container" else {})}
        for key, prop in typed.items():
            if key in attrib:
                kind = "int" if WIDGET_PROPS[prop]["type"] == "int" else "string"
                put(prop, _typed_value(resolve(attrib[key], args, path, key, kind), prop, key, path), key)
        out = {"id": node_id, "type": widget, "props": props, "children": []}
        bindings = []
        if widget == "label":
            text = resolve(attrib.get("text", ""), args, path, "text", "string")
            if len(text.encode("utf-8")) > MAX_TEXT:
                _fail("UI_TEXT_SIZE", path, f"text exceeds {MAX_TEXT} bytes")
            props["text"] = text
            if "bind_text" in attrib:
                bindings.append(_binding(widget, "text", attrib["bind_text"], "bind_text", path))
        if "bind_value" in attrib:
            bindings.append(_binding(widget, BINDABLE[widget][0], attrib["bind_value"], "bind_value", path))
        if widget == "image":
            name = attrib.get("src")
            if not name:
                _fail("UI_ASSET_MISSING", path, "lv_image requires src")
            if name not in images:
                _fail("UI_ASSET_MISSING", path, f"image {name} is not declared in globals.xml")
            if len(assets) >= MAX_ASSETS:
                _fail("UI_ASSET_LIMIT", path, f"asset count exceeds {MAX_ASSETS}")
            assets[node_id] = _asset(ui_dir, images[name], path)
            props["src"] = images[name]
            props["asset_id"] = node_id
        series, cells = [], {}
        for child in children:
            child_path = f"{path}/{child.tag}"
            if child.tag == "event_cb":
                if (not spec["requires_action"] or set(child.attrib) != {"trigger", "callback", "user_data"}
                        or len(child)):
                    _fail("UI_EVENT", child_path, "buttons take <event_cb trigger callback user_data/> only")
                if child.get("trigger") != ACTION_TRIGGER or child.get("callback") != ACTION_CALLBACK:
                    _fail("UI_EVENT", child_path,
                          f'use trigger="{ACTION_TRIGGER}" callback="{ACTION_CALLBACK}"')
                if child.get("user_data") not in ACTION_IDS:
                    _fail("UI_ACTION", child_path, f"unregistered action id {child.get('user_data')}")
                if "action" in out:
                    _fail("UI_EVENT", child_path, "a button supports one action")
                out["action"] = {"event": "activate", "id": child.get("user_data")}
                continue
            if widget == "chart" and child.tag == "lv_chart-series":
                # <lv_chart-series [values="1 2 3"] [bind_values="value.N"]/>, at most series_count.
                if not set(child.attrib) <= {"values", "bind_values"} or len(child):
                    _fail("UI_PROPERTY", child_path, "a series takes values and bind_values only")
                series.append(child)
                prop = f"series_{len(series)}"
                if prop not in WIDGET_PROPS:
                    _fail("UI_PROPERTY_VALUE", child_path, "a chart has at most 2 series")
                if "values" in child.attrib:
                    props[prop] = _typed_value(child.get("values"), prop, "values", child_path)
                if "bind_values" in child.attrib:
                    bindings.append(_binding(widget, prop, child.get("bind_values"), "bind_values", child_path))
                continue
            if widget == "table" and child.tag in ("lv_table-cell", "lv_table-row"):
                # <lv_table-cell row col value/> sets a cell; <lv_table-row row bind_value/> binds a row,
                # its cells separated by "|".
                expected = {"row", "column", "value"} if child.tag == "lv_table-cell" else {"row", "bind_value"}
                if set(child.attrib) != expected or len(child):
                    _fail("UI_PROPERTY", child_path, f"{child.tag} takes {', '.join(sorted(expected))}")
                try:
                    row = int(child.get("row"))
                    col = int(child.get("column", "0"))
                except ValueError:
                    _fail("UI_PROPERTY_VALUE", child_path, "row and column are integers")
                if not 0 <= row < props.get("rows", 1) or not 0 <= col < props.get("cols", 1):
                    _fail("UI_PROPERTY_VALUE", child_path, "the cell is outside row_count x column_count")
                if child.tag == "lv_table-row":
                    bindings.append(_binding(widget, f"row_{row}", child.get("bind_value"), "bind_value", child_path))
                else:
                    text = child.get("value")
                    if len(text.encode("utf-8")) > WIDGET_PROPS["cells"]["max_bytes"]:
                        _fail("UI_TEXT_SIZE", child_path, f"a cell is at most {WIDGET_PROPS['cells']['max_bytes']} bytes")
                    cells[(row, col)] = text
                continue
            out["children"].append(instantiate(child, scope, args, depth + 1, child_path, ancestry))
        if series:
            props["series_count"] = len(series)
        if cells:
            last = max(r * props.get("cols", 1) + c for r, c in cells)
            props["cells"] = [cells.get(divmod(i, props.get("cols", 1)), "") for i in range(last + 1)]
        if len({b["property"] for b in bindings}) != len(bindings):
            _fail("UI_BINDING", path, "a property is bound twice")
        if len(bindings) == 1:
            out["binding"] = bindings[0]
        elif bindings:
            out["bindings"] = bindings
        _check_combination(widget, props, path)
        if not spec["children"] and out["children"]:
            _fail("UI_CHILD", path, f"{tag} cannot contain widgets")
        if spec["requires_action"] and "action" not in out:
            _fail("UI_ACTION", path, "lv_button requires an <event_cb> with a registered action")
        return out

    def instantiate(node: ET.Element, scope: str, args: dict, depth: int, path: str,
                    ancestry: tuple) -> dict:
        # Resolve the element type first, so unsupported LVGL elements report UI_WIDGET.
        definition = None if node.tag in LVGL_WIDGETS else component(node.tag, path)
        name = node.get("name")
        if not NAME_RE.fullmatch(name or ""):
            _fail("UI_NODE_ID", path, "every widget needs a name (lowercase letters, digits, _)")
        attrib = {k: v for k, v in node.attrib.items() if k != "name"}
        if definition is None:
            return build(node.tag, attrib, list(node), scope + name, scope, args, depth, path, ancestry)
        props, view, label = definition
        if node.tag in ancestry:
            _fail("UI_COMPONENT_CYCLE", path, f"recursive component {node.tag}")
        if len(node):
            _fail("UI_CHILD", path, "component instances cannot have children")
        instance_args = {}
        for prop, (prop_type, default) in props.items():
            value = attrib.pop(prop, default)
            if value is None:
                _fail("UI_COMPONENT_REF", path, f"{node.tag} requires {prop}")
            instance_args[prop] = (prop_type, resolve(value, args, path, prop, prop_type))
        layout = {k: attrib.pop(k) for k in LAYOUT_ATTRS if k in attrib}
        if attrib:
            _fail("UI_PROPERTY", path, f"{node.tag} has no prop {sorted(attrib)[0]}")
        base = view.get("extends", "lv_obj")
        if base not in LVGL_WIDGETS:
            _fail("UI_COMPONENT", label, f"view extends unsupported widget {base}")
        view_attrib = {k: v for k, v in view.attrib.items() if k != "extends"}
        view_attrib.update(layout)
        # The instance's view is the node <scope><name>; names inside it are <scope><name>.<child>.
        node_id = scope + name
        return build(base, view_attrib, list(view), node_id, node_id + ".", instance_args, depth,
                     f"{path}<{label}>", ancestry + (node.tag,))

    label = f"screens/{screen}.xml"
    root = _read_xml(ui_dir / "screens" / f"{screen}.xml", label, budget)
    if root.tag != "screen" or root.attrib:
        _fail("UI_SCREEN", label, "expected a <screen> root without attributes")
    sections = [child for child in root if child.tag != "previews"]
    if len(sections) != 1 or sections[0].tag != "view":
        _fail("UI_SCREEN", label, "a screen contains exactly one <view>")
    view = sections[0]
    if "name" in view.attrib or "extends" in view.attrib:
        _fail("UI_SCREEN", f"{label}/view", "the screen view takes its name from the file")
    tree = build("lv_obj", dict(view.attrib), list(view), screen, "", {}, 1, f"{label}/view", ())
    if sum(asset["bytes"] for asset in assets.values()) > MAX_ASSET_BYTES:
        _fail("UI_ASSET_SIZE", label, "total declared asset bytes exceed the profile")
    normalized = {"format": FORMAT, "version": FORMAT_VERSION, "screen": screen, "tree": tree,
                  "assets": assets, "limits": {"nodes": count, "max_depth": MAX_DEPTH}}
    if len(json.dumps(normalized, separators=(",", ":")).encode("utf-8")) > MAX_XML_BYTES:
        _fail("UI_REPRESENTATION_SIZE", label, "compiled screen exceeds the device limit")
    return normalized


def validate_update(update: dict, generation: int, active: bool, document: dict) -> None:
    if not active or update.get("generation") != generation:
        _fail("UI_UPDATE_STALE", "/update/generation", "screen is closed or update generation is stale")
    if set(update) != {"generation", "id", "key", "type", "value"}:
        _fail("UI_UPDATE_SCHEMA", "/update", "unexpected update fields")
    if (update["key"] not in BINDING_KEYS or update["type"] != BINDING_TYPE
            or not isinstance(update["value"], str)):
        _fail("UI_UPDATE_TYPE", "/update", "expected a registered binding key and type")
    if len(update["value"].encode("utf-8")) > MAX_TEXT or not ID_RE.fullmatch(update["id"]):
        _fail("UI_UPDATE_VALUE", "/update", "invalid node id or oversized value")
    pending = [document["tree"]]
    while pending:
        node = pending.pop()
        if node["id"] == update["id"]:
            for binding in ([node["binding"]] if "binding" in node else node.get("bindings", [])):
                if binding["key"] == update["key"] and binding["type"] == update["type"]:
                    return
            break
        pending.extend(node["children"])
    _fail("UI_UPDATE_REFERENCE", "/update/id", "node is not bound to a registered binding with this key")


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ui_dir", type=Path, help="plugin UI directory (screens/, components/, globals.xml)")
    parser.add_argument("screen", help="screen to compile: screens/<screen>.xml")
    parser.add_argument("output", type=Path)
    args = parser.parse_args(argv)
    try:
        normalized = compile_package(args.ui_dir, args.screen)
        args.output.write_text(json.dumps(normalized, separators=(",", ":")) + "\n", encoding="utf-8")
    except (OSError, UIError) as exc:
        print(str(exc), file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
