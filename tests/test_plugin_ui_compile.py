import json
import struct
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools"))
from plugin_ui_compile import UIError, compile_xml, validate_update
from plugin_ui_registry import load_registry, main as registry_main, render_header


XML = b'''<plugin-ui version="1"><components><component id="card"><container id="root" background="#101820"><label id="title" text="Hello"><bind property="text" key="title" type="string"/></label></container></component></components><screen id="demo"><container id="main" width="240" height="320"><use component="card" id="first"/><image id="logo" src="assets/logo.rgb565"/><button id="increment"><label id="caption" text="Add"/><on event="activate" action="plugin.increment"/></button></container></screen></plugin-ui>'''


class PluginUiCompileTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        (self.root / "assets").mkdir()
        (self.root / "assets/logo.rgb565").write_bytes(struct.pack("<HHHH", 2, 1, 0xF800, 0x07E0))

    def tearDown(self):
        self.tmp.cleanup()

    def compile(self, xml=XML):
        return compile_xml(xml, self.root)

    def rejected(self, xml, code):
        with self.assertRaises(UIError) as ctx:
            self.compile(xml)
        self.assertEqual(ctx.exception.code, code)
        self.assertIn("/", ctx.exception.path)

    def test_registry_header_generation_and_check(self):
        header = self.root / "plugin_ui_registry.generated.h"
        self.assertEqual(registry_main(["--output", str(header)]), 0)
        self.assertEqual(header.read_text(encoding="utf-8"), render_header(load_registry()))
        self.assertEqual(registry_main(["--output", str(header), "--check"]), 0)
        header.write_text("stale", encoding="utf-8")
        self.assertEqual(registry_main(["--output", str(header), "--check"]), 1)

    def test_valid_screen_normalizes_components_bindings_actions_and_asset(self):
        result = self.compile()
        self.assertEqual(result["format"], "aitbase.plugin-ui")
        self.assertEqual(result["version"], 1)
        self.assertEqual(result["tree"]["children"][0]["id"], "first.root")
        self.assertEqual(result["tree"]["children"][0]["children"][0]["binding"]["key"], "title")
        self.assertEqual(result["tree"]["children"][2]["action"]["id"], "plugin.increment")
        self.assertEqual(result["assets"]["logo"]["format"], "rgb565")
        self.assertEqual(json.loads(json.dumps(result)), result)

    def test_malformed_xml_has_stable_code_and_location(self):
        self.rejected(b"<plugin-ui", "UI_XML_SYNTAX")

    def test_rejects_schema_version_and_dtd(self):
        self.rejected(XML.replace(b'version="1"', b'version="2"'), "UI_SCHEMA_VERSION")
        self.rejected(b'<!DOCTYPE x [<!ENTITY a "b">]>' + XML, "UI_XML_DTD")
        self.rejected((b'<!DOCTYPE x [<!ENTITY a "b">]>' + XML).decode().encode("utf-16"), "UI_XML_ENCODING")

    def test_rejects_unknown_widget_property_event_binding_and_action(self):
        self.rejected(XML.replace(b'<container id="root"', b'<native id="root"').replace(b'</container>', b'</native>', 1), "UI_WIDGET")
        self.rejected(XML.replace(b'width="240"', b'callback="anything" width="240"'), "UI_PROPERTY")
        self.rejected(XML.replace(b'width="240"', b'width="-1"'), "UI_PROPERTY_VALUE")
        self.rejected(XML.replace(b'event="activate"', b'event="exec"'), "UI_EVENT")
        self.rejected(XML.replace(b'key="title"', b'key="secret"'), "UI_BINDING")
        self.rejected(XML.replace(b'action="plugin.increment"', b'action="shell.run"'), "UI_ACTION")

    def test_rejects_depth_and_node_limits(self):
        body = b"".join(f"<container id='n{i}'>".encode() for i in range(7)) + b"<label id='last'/>" + b"</container>" * 7
        xml = XML.replace(b'<container id="main" width="240" height="320"><use component="card" id="first"/><image id="logo" src="assets/logo.rgb565"/><button id="increment"><label id="caption" text="Add"/><on event="activate" action="plugin.increment"/></button></container>', body)
        self.rejected(xml, "UI_DEPTH")
        labels = b"".join(f'<label id="n{i}"/>'.encode() for i in range(35))
        xml = XML.replace(b'<use component="card" id="first"/>', labels)
        self.rejected(xml, "UI_NODE_LIMIT")

    def test_rejects_oversized_document_and_asset(self):
        self.rejected(XML + b" " * (16 * 1024), "UI_XML_SIZE")
        (self.root / "assets/logo.rgb565").write_bytes(b"x" * (8 * 1024 + 1))
        self.rejected(XML, "UI_ASSET_SIZE")

    def test_rejects_representation_expansion_over_runtime_cap(self):
        screen = [f'<image id="i{i}" src="assets/i{i}.rgb565"/>' for i in range(4)]
        for i in range(4):
            (self.root / f"assets/i{i}.rgb565").write_bytes(struct.pack("<HH", 31, 32) + b"\0" * (31 * 32 * 2))
        xml = ("<plugin-ui version='1'><components><component id='card'><label id='copy'/></component></components>"
               "<screen id='demo'><container id='root'>" + "".join(screen) + "</container></screen></plugin-ui>").encode()
        self.rejected(xml, "UI_REPRESENTATION_SIZE")

    def test_rejects_missing_invalid_dimensions_and_unsupported_asset(self):
        self.rejected(XML.replace(b"assets/logo.rgb565", b"assets/missing.rgb565"), "UI_ASSET_MISSING")
        (self.root / "assets/logo.rgb565").write_bytes(struct.pack("<HH", 33, 1) + b"\0" * 66)
        self.rejected(XML, "UI_ASSET_DIMENSIONS")
        (self.root / "assets/logo.png").write_bytes(b"not-image")
        self.rejected(XML.replace(b"assets/logo.rgb565", b"assets/logo.png"), "UI_ASSET_FORMAT")

    def test_rejects_traversal_external_urls_and_native_callback_fields(self):
        self.rejected(XML.replace(b"assets/logo.rgb565", b"../../etc/passwd"), "UI_ASSET_PATH")
        self.rejected(XML.replace(b"assets/logo.rgb565", b"https://example.invalid/logo.png"), "UI_ASSET_PATH")
        self.rejected(XML.replace(b'<on event="activate" action="plugin.increment"/>', b'<on event="activate" action="plugin.increment" callback="lv_obj_del"/>'), "UI_EVENT")
        self.rejected(XML.replace(b'<on event="activate" action="plugin.increment"/>', b'<pointer value="0x1234"/>'), "UI_WIDGET")

    def test_rejects_duplicate_ids_and_bad_component_reference(self):
        self.rejected(XML.replace(b'<image id="logo"', b'<image id="first.root"'), "UI_DUPLICATE_ID")
        self.rejected(XML.replace(b'component="card"', b'component="missing"'), "UI_COMPONENT_REF")
        recursive = XML.replace(
            b'<container id="root" background="#101820"><label id="title" text="Hello"><bind property="text" key="title" type="string"/></label></container>',
            b'<use component="card" id="nested"/>')
        self.rejected(recursive, "UI_COMPONENT_CYCLE")

    def test_rejects_symlink_asset_path(self):
        (self.root / "assets/link.rgb565").symlink_to("logo.rgb565")
        self.rejected(XML.replace(b"assets/logo.rgb565", b"assets/link.rgb565"), "UI_ASSET_PATH")

    def test_update_is_typed_generation_scoped_and_bounded(self):
        update = {"generation": 4, "id": "first.title", "key": "title", "type": "string", "value": "Updated"}
        document = self.compile()
        validate_update(update, 4, True, document)
        for generation, active, code in ((3, True, "UI_UPDATE_STALE"), (4, False, "UI_UPDATE_STALE")):
            with self.assertRaises(UIError) as ctx:
                validate_update({**update, "generation": generation}, 4, active, document)
            self.assertEqual(ctx.exception.code, code)
        with self.assertRaises(UIError) as ctx:
            validate_update({**update, "type": "pointer"}, 4, True, document)
        self.assertEqual(ctx.exception.code, "UI_UPDATE_TYPE")
        with self.assertRaises(UIError) as ctx:
            validate_update({**update, "value": "x" * 81}, 4, True, document)
        self.assertEqual(ctx.exception.code, "UI_UPDATE_VALUE")
        with self.assertRaises(UIError) as ctx:
            validate_update({**update, "id": "caption"}, 4, True, document)
        self.assertEqual(ctx.exception.code, "UI_UPDATE_REFERENCE")


if __name__ == "__main__":
    unittest.main()
