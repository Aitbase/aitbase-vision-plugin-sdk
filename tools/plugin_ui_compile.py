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
BINDINGS = REGISTRY["bindings"]
ID_RE = re.compile(r"^[a-z][a-z0-9_.-]{0,%d}$" % (LIMITS["max_id"] - 1))


def _binding_for(widget: str, attrib: dict):
    for binding in BINDINGS:
        if binding["widget"] == widget and attrib == {k: binding[k] for k in ("property", "key", "type")}:
            return {k: binding[k] for k in ("property", "key", "type")}
    return None


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


# LVGL 9.4 widget tags accepted by the compiler, mapped to the neutral registry widget names.
LVGL_WIDGETS = {"lv_obj": "container", "lv_label": "label", "lv_button": "button", "lv_image": "image"}
# LVGL style attributes mapped to registry properties.
STYLE_ATTRS = {"style_pad_all": "padding", "style_bg_color": "background", "style_text_color": "color"}
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


def _color_value(raw: str, key: str, path: str) -> str:
    if raw.startswith("#"):
        if raw[1:] not in TOKEN_NAMES:
            _fail("UI_STYLE", path, f"{key}: unknown color token {raw} (use one of "
                  + ", ".join("#" + name for name in TOKEN_NAMES) + ")")
        return TOKEN_NAMES[raw[1:]]
    literal = "#" + raw[2:].lower() if raw.lower().startswith("0x") else None
    if literal not in TOKEN_BY_VALUE:
        _fail("UI_STYLE", path, f"{key}: colors must be platform tokens such as #{next(iter(TOKEN_NAMES))}")
    return literal


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
        allowed = set(LAYOUT_ATTRS) | set(STYLE_ATTRS)
        if widget == "label":
            allowed |= {"text", "bind_text"}
        if widget == "image":
            allowed |= {"src"}
        unknown = sorted(set(attrib) - allowed)
        if unknown:
            _fail("UI_PROPERTY", path, f"unsupported attribute {unknown[0]}")
        props = {}
        for key in LAYOUT_ATTRS:
            if key in attrib:
                props[key] = _int_value(resolve(attrib[key], args, path, key, "int"), key, path)
        if "style_pad_all" in attrib:
            props["padding"] = _int_value(resolve(attrib["style_pad_all"], args, path, "style_pad_all", "int"),
                                          "padding", path)
        for key in ("style_bg_color", "style_text_color"):
            if key in attrib:
                props[STYLE_ATTRS[key]] = _color_value(resolve(attrib[key], args, path, key, "color"), key, path)
        out = {"id": node_id, "type": widget, "props": props, "children": []}
        if widget == "label":
            text = resolve(attrib.get("text", ""), args, path, "text", "string")
            if len(text.encode("utf-8")) > MAX_TEXT:
                _fail("UI_TEXT_SIZE", path, f"text exceeds {MAX_TEXT} bytes")
            props["text"] = text
            if "bind_text" in attrib:
                binding = _binding_for(widget, {"property": "text", "key": attrib["bind_text"], "type": "string"})
                if binding is None:
                    _fail("UI_BINDING", path, f"bind_text subject {attrib['bind_text']} is not a platform subject")
                out["binding"] = binding
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
            out["children"].append(instantiate(child, scope, args, depth + 1, child_path, ancestry))
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
    if (not any(update["key"] == b["key"] and update["type"] == b["type"] for b in BINDINGS)
            or not isinstance(update["value"], str)):
        _fail("UI_UPDATE_TYPE", "/update", "expected a registered binding key and type")
    if len(update["value"].encode("utf-8")) > MAX_TEXT or not ID_RE.fullmatch(update["id"]):
        _fail("UI_UPDATE_VALUE", "/update", "invalid node id or oversized value")
    pending = [document["tree"]]
    while pending:
        node = pending.pop()
        if node["id"] == update["id"]:
            binding = node.get("binding")
            if binding and binding["key"] == update["key"] and binding["type"] == update["type"]:
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
