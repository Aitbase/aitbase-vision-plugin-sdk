#!/usr/bin/env python3
"""Compile the XML UI PoC subset to the versioned trusted-renderer format."""

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


def compile_xml(xml: bytes, asset_root: Path) -> dict:
    if len(xml) > MAX_XML_BYTES:
        _fail("UI_XML_SIZE", "/", f"document exceeds {MAX_XML_BYTES} bytes")
    try:
        xml.decode("utf-8")
    except UnicodeDecodeError:
        _fail("UI_XML_ENCODING", "/", "only UTF-8 XML is supported")
    # The firmware loader does not reject DTDs; reject them before the host parser sees them.
    if re.search(br"<!\s*(DOCTYPE|ENTITY)\b", xml, re.IGNORECASE):
        _fail("UI_XML_DTD", "/", "DTD and entity declarations are not supported")
    try:
        root = ET.fromstring(xml)
    except ET.ParseError as exc:
        line, col = exc.position
        _fail("UI_XML_SYNTAX", f"/line/{line}/column/{col}", str(exc))
    if root.tag != "plugin-ui" or root.attrib != {"version": "1"}:
        _fail("UI_SCHEMA_VERSION", "/plugin-ui", "expected plugin-ui version=\"1\"")

    parts = [child.tag for child in root]
    if sorted(parts) != ["components", "screen"]:
        _fail("UI_SCHEMA", "/plugin-ui", "expected one components section and one screen")
    component_section = next(child for child in root if child.tag == "components")
    screen_section = next(child for child in root if child.tag == "screen")
    components = {}
    for comp in component_section:
        path = f"/plugin-ui/components/{comp.tag}"
        if comp.tag != "component" or set(comp.attrib) != {"id"} or not ID_RE.fullmatch(comp.get("id", "")):
            _fail("UI_COMPONENT", path, "component requires a valid id")
        name = comp.get("id")
        if name in components:
            _fail("UI_DUPLICATE_ID", path, f"duplicate component id {name}")
        if len(comp) != 1:
            _fail("UI_COMPONENT", path, "component must contain exactly one widget")
        components[name] = comp[0]
    if not components:
        _fail("UI_COMPONENT", "/plugin-ui/components", "at least one reusable component is required")
    if screen_section.tag != "screen" or set(screen_section.attrib) != {"id"} or not ID_RE.fullmatch(screen_section.get("id", "")):
        _fail("UI_SCREEN", "/plugin-ui/screen", "screen requires a valid id")
    if len(screen_section) != 1:
        _fail("UI_SCREEN", "/plugin-ui/screen", "screen must contain exactly one root widget")

    ids, assets, count = set(), {}, 0

    def expand(node: ET.Element, prefix: str, depth: int, ancestry: tuple[str, ...], path: str):
        nonlocal count
        if depth > MAX_DEPTH:
            _fail("UI_DEPTH", path, f"nesting exceeds {MAX_DEPTH}")
        if node.tag == "use":
            if set(node.attrib) != {"component", "id"} or len(node):
                _fail("UI_COMPONENT_REF", path, "use requires component and id only")
            name, local_id = node.get("component"), node.get("id")
            if name not in components:
                _fail("UI_COMPONENT_REF", path, f"unknown component {name}")
            if name in ancestry:
                _fail("UI_COMPONENT_CYCLE", path, f"recursive component {name}")
            if not ID_RE.fullmatch(local_id or ""):
                _fail("UI_NODE_ID", path, "invalid component instance id")
            return expand(components[name], f"{prefix}{local_id}.", depth, ancestry + (name,), path + f"/use[{name}]")
        if node.tag not in WIDGETS:
            _fail("UI_WIDGET", path, f"unknown widget {node.tag}")
        count += 1
        if count > MAX_NODES:
            _fail("UI_NODE_LIMIT", path, f"expanded widget count exceeds {MAX_NODES}")
        spec = WIDGETS[node.tag]
        allowed = ATTRS | set(spec["props"])
        unknown = set(node.attrib) - allowed
        if unknown:
            key = sorted(unknown)[0]
            _fail("UI_PROPERTY", path, f"unknown property {key}")
        node_id = node.get("id")
        if not ID_RE.fullmatch(node_id or ""):
            _fail("UI_NODE_ID", path, "widget requires a valid id")
        node_id = prefix + node_id
        if node_id in ids:
            _fail("UI_DUPLICATE_ID", path, f"duplicate node id {node_id}")
        ids.add(node_id)
        props = {}
        for key, (low, high) in INT_PROPS.items():
            if key in node.attrib:
                try:
                    value = int(node.get(key))
                except ValueError:
                    _fail("UI_PROPERTY_VALUE", path, f"{key} must be an integer")
                if not low <= value <= high:
                    _fail("UI_PROPERTY_VALUE", path, f"{key} must be within [{low}, {high}]")
                props[key] = value
        for key in sorted(COLOR_PROPS):
            if key in node.attrib:
                if node.get(key) not in STYLE_COLORS:
                    _fail("UI_STYLE", path, f"unsupported color token {node.get(key)}")
                props[key] = node.get(key)
        if "text" in spec["props"]:
            props["text"] = node.get("text", "")
            if len(props["text"].encode("utf-8")) > MAX_TEXT:
                _fail("UI_TEXT_SIZE", path, f"text exceeds {MAX_TEXT} bytes")
        if "src" in spec["props"]:
            source = node.get("src")
            if not source:
                _fail("UI_ASSET_MISSING", path, "image requires src")
            if len(assets) >= MAX_ASSETS:
                _fail("UI_ASSET_LIMIT", path, f"asset count exceeds {MAX_ASSETS}")
            assets[node_id] = _asset(asset_root, source, path)
            props["src"] = source
            props["asset_id"] = node_id
        out = {"id": node_id, "type": node.tag, "props": props, "children": []}
        for child in node:
            child_path = path + "/" + child.tag
            if child.tag == "bind":
                binding = _binding_for(node.tag, dict(child.attrib))
                if binding is None or len(child):
                    _fail("UI_BINDING", child_path, "binding is not registered for this widget")
                if "binding" in out:
                    _fail("UI_BINDING", child_path, "duplicate binding")
                out["binding"] = binding
                continue
            if child.tag == "on":
                if not spec["requires_action"] or set(child.attrib) != {"event", "action"} or len(child):
                    _fail("UI_EVENT", child_path, "button event requires event and action only")
                if child.get("event") not in EVENTS:
                    _fail("UI_EVENT", child_path, f"unknown event {child.get('event')}")
                action = child.get("action")
                if action not in ACTION_IDS:
                    _fail("UI_ACTION", child_path, f"unregistered action id {action}")
                if "action" in out:
                    _fail("UI_EVENT", child_path, "button supports one action")
                out["action"] = {"event": child.get("event"), "id": action}
                continue
            out["children"].append(expand(child, prefix, depth + 1, ancestry, child_path))
        if not spec["children"] and out["children"]:
            _fail("UI_CHILD", path, f"{node.tag} cannot contain widgets")
        if spec["requires_action"] and "action" not in out:
            _fail("UI_ACTION", path, "button requires a registered click action")
        return out

    tree = expand(screen_section[0], "", 1, (), "/plugin-ui/screen")
    if sum(asset["bytes"] for asset in assets.values()) > MAX_ASSET_BYTES:
        _fail("UI_ASSET_SIZE", "/plugin-ui/screen", "total declared asset bytes exceed the PoC profile")
    normalized = {"format": FORMAT, "version": FORMAT_VERSION,
                  "screen": screen_section.get("id"), "tree": tree, "assets": assets,
                  "limits": {"nodes": count, "max_depth": MAX_DEPTH}}
    if len(json.dumps(normalized, separators=(",", ":")).encode("utf-8")) > MAX_XML_BYTES:
        _fail("UI_REPRESENTATION_SIZE", "/plugin-ui", "normalized representation exceeds the runtime limit")
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
    parser.add_argument("xml", type=Path)
    parser.add_argument("package", type=Path, help="directory containing declared RGB565 assets")
    parser.add_argument("output", type=Path)
    args = parser.parse_args(argv)
    try:
        normalized = compile_xml(args.xml.read_bytes(), args.package)
        args.output.write_text(json.dumps(normalized, separators=(",", ":")) + "\n", encoding="utf-8")
    except (OSError, UIError) as exc:
        print(str(exc), file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
