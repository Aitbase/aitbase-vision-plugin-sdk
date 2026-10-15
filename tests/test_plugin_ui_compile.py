import json
import struct
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools"))
from plugin_ui_compile import UIError, compile_package, validate_update  # noqa: E402
from plugin_ui_registry import load_registry, main as registry_main, render_globals, render_header  # noqa: E402

GLOBALS = '<globals><images><file name="logo" src_path="assets/logo.rgb565"/></images></globals>'
CARD = '''<component>
  <api><prop name="title" type="string" default="Hello"/></api>
  <view style_bg_color="#surface" style_pad_all="8">
    <lv_label name="title" text="$title" style_text_color="#on_surface" bind_text="title"/>
  </view>
  <previews><preview width="240" height="80"/></previews>
</component>'''
BUTTON = ('<lv_button name="increment" style_bg_color="#control" style_pad_all="8">'
          '<lv_label name="caption" text="Add" style_text_color="#accent"/>'
          '<event_cb trigger="clicked" callback="aitbase_action" user_data="plugin.increment"/>'
          '</lv_button>')
SCREEN = ('<screen><view width="240" height="320px" style_bg_color="#surface">'
          '<card name="status" title="Count: 0"/><lv_image name="logo" src="logo"/>' + BUTTON +
          '</view></screen>')


class PluginUiCompileTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        for sub in ("assets", "screens", "components"):
            (self.root / sub).mkdir()
        (self.root / "assets/logo.rgb565").write_bytes(struct.pack("<HHHH", 2, 1, 0xF800, 0x07E0))
        self.write(globals_xml=GLOBALS, card=CARD, screen=SCREEN)

    def tearDown(self):
        self.tmp.cleanup()

    def write(self, screen=None, card=None, globals_xml=None, name="demo"):
        if screen is not None:
            (self.root / f"screens/{name}.xml").write_text(screen, encoding="utf-8")
        if card is not None:
            (self.root / "components/card.xml").write_text(card, encoding="utf-8")
        if globals_xml is not None:
            (self.root / "globals.xml").write_text(globals_xml, encoding="utf-8")

    def compile(self, screen="demo"):
        return compile_package(self.root, screen)

    def rejected(self, code, screen=None, card=None, globals_xml=None):
        self.write(screen=screen, card=card, globals_xml=globals_xml)
        try:
            with self.assertRaises(UIError) as ctx:
                self.compile()
            self.assertEqual(ctx.exception.code, code, str(ctx.exception))
        finally:
            self.write(screen=SCREEN, card=CARD, globals_xml=GLOBALS)

    def test_registry_header_and_globals_generation_and_check(self):
        header = self.root / "plugin_ui_registry.generated.h"
        self.assertEqual(registry_main(["--output", str(header)]), 0)
        self.assertEqual(header.read_text(encoding="utf-8"), render_header(load_registry()))
        self.assertEqual(registry_main(["--output", str(header), "--check"]), 0)
        header.write_text("stale", encoding="utf-8")
        self.assertEqual(registry_main(["--output", str(header), "--check"]), 1)

    def test_checked_in_platform_globals_match_registry(self):
        globals_path = Path(__file__).resolve().parents[1] / "schema" / "aitbase_globals.xml"
        self.assertEqual(globals_path.read_text(encoding="utf-8"), render_globals(load_registry()),
                         "run tools/plugin_ui_registry.py --globals schema/aitbase_globals.xml")

    def test_valid_package_compiles_to_the_neutral_device_format(self):
        result = self.compile()
        self.assertEqual((result["format"], result["version"], result["screen"]), ("aitbase.plugin-ui", 1, "demo"))
        tree = result["tree"]
        self.assertEqual((tree["id"], tree["type"]), ("demo", "container"))
        self.assertEqual(tree["props"], {"width": 240, "height": 320, "background": "#101820"})
        card, image, button = tree["children"]
        self.assertEqual((card["id"], card["type"], card["props"]), ("status", "container", {"padding": 8, "background": "#101820"}))
        title = card["children"][0]
        self.assertEqual(title["id"], "status.title")
        self.assertEqual(title["props"], {"color": "#ffffff", "text": "Count: 0"})
        self.assertEqual(title["binding"], {"property": "text", "key": "title", "type": "string"})
        self.assertEqual(image["props"], {"src": "assets/logo.rgb565", "asset_id": "logo"})
        self.assertEqual(result["assets"]["logo"]["format"], "rgb565")
        self.assertEqual(button["action"], {"event": "activate", "id": "plugin.increment"})
        self.assertEqual(button["children"][0]["props"]["color"], "#00e5ff")
        self.assertEqual(result["limits"]["nodes"], 6)
        self.assertEqual(json.loads(json.dumps(result)), result)

    def test_component_prop_default_and_literal_token_color(self):
        self.write(screen=SCREEN.replace(' title="Count: 0"', '').replace('"#surface">', '"0x101820">', 1))
        result = self.compile()
        self.assertEqual(result["tree"]["children"][0]["children"][0]["props"]["text"], "Hello")
        self.assertEqual(result["tree"]["props"]["background"], "#101820")

    def test_rejects_malformed_xml_dtd_and_encoding(self):
        self.rejected("UI_XML_SYNTAX", screen="<screen")
        self.rejected("UI_XML_DTD", screen='<!DOCTYPE x [<!ENTITY a "b">]>' + SCREEN)
        (self.root / "screens/demo.xml").write_bytes(SCREEN.encode("utf-16"))
        with self.assertRaises(UIError) as ctx:
            self.compile()
        self.assertEqual(ctx.exception.code, "UI_XML_ENCODING")

    def test_rejects_unknown_widgets_attributes_and_values(self):
        self.rejected("UI_WIDGET", screen=SCREEN.replace('<lv_image name="logo" src="logo"/>', '<lv_slider name="s"/>'))
        self.rejected("UI_WIDGET", screen=SCREEN.replace('<lv_image name="logo" src="logo"/>', '<Card name="s"/>'))
        self.rejected("UI_PROPERTY", screen=SCREEN.replace('width="240"', 'hidden="true" width="240"'))
        self.rejected("UI_PROPERTY", screen=SCREEN.replace('width="240"', 'style_shadow_width="4" width="240"'))
        self.rejected("UI_PROPERTY", screen=SCREEN.replace('width="240"', 'align="center" width="240"'))
        self.rejected("UI_PROPERTY_VALUE", screen=SCREEN.replace('width="240"', 'width="-1"'))
        self.rejected("UI_PROPERTY_VALUE", screen=SCREEN.replace('width="240"', 'width="50%"'))

    def test_colors_must_be_platform_tokens(self):
        self.rejected("UI_STYLE", screen=SCREEN.replace('"#surface"', '"#brand"', 1))
        self.rejected("UI_STYLE", screen=SCREEN.replace('"#surface"', '"0x123456"', 1))
        self.rejected("UI_STYLE", screen=SCREEN.replace('"#surface"', '"red"', 1))

    def test_events_map_only_registered_actions(self):
        event = '<event_cb trigger="clicked" callback="aitbase_action" user_data="plugin.increment"/>'
        self.rejected("UI_EVENT", screen=SCREEN.replace('trigger="clicked"', 'trigger="pressed"'))
        self.rejected("UI_EVENT", screen=SCREEN.replace('callback="aitbase_action"', 'callback="lv_obj_delete"'))
        self.rejected("UI_ACTION", screen=SCREEN.replace('user_data="plugin.increment"', 'user_data="shell.run"'))
        self.rejected("UI_ACTION", screen=SCREEN.replace(event, ''))
        self.rejected("UI_EVENT", screen=SCREEN.replace(event, event * 2))
        self.rejected("UI_WIDGET", screen=SCREEN.replace(event, '<screen_load_event screen="settings"/>' + event))
        self.rejected("UI_EVENT", card=CARD.replace('bind_text="title"/>', 'bind_text="title">' + event + '</lv_label>'))

    def test_bindings_must_be_platform_subjects(self):
        self.rejected("UI_BINDING", card=CARD.replace('bind_text="title"', 'bind_text="secret"'))
        self.rejected("UI_PROPERTY", card=CARD.replace('bind_text="title"', 'bind_text="title" bind_text-fmt="%s"'))

    def test_names_are_required_and_unique(self):
        self.rejected("UI_NODE_ID", screen=SCREEN.replace('<lv_image name="logo"', '<lv_image'))
        self.rejected("UI_NODE_ID", screen=SCREEN.replace('name="logo"', 'name="Logo"'))
        self.rejected("UI_DUPLICATE_ID", screen=SCREEN.replace('name="caption"', 'name="increment"'))
        self.rejected("UI_SCREEN", screen=SCREEN.replace('<view width', '<view name="x" width'))

    def test_component_props_are_declared_and_typed(self):
        self.rejected("UI_COMPONENT_REF", card=CARD.replace('text="$title"', 'text="$missing"'))
        self.rejected("UI_COMPONENT_REF", card=CARD.replace('style_pad_all="8"', 'style_pad_all="$title"'))
        self.rejected("UI_PROPERTY", screen=SCREEN.replace('title="Count: 0"', 'title="x" callback="y"'))
        self.rejected("UI_CHILD", screen=SCREEN.replace('<card name="status" title="Count: 0"/>',
                                                        '<card name="status"><lv_label name="x"/></card>'))
        self.rejected("UI_COMPONENT_REF", card=CARD.replace(' default="Hello"', ''),
                      screen=SCREEN.replace(' title="Count: 0"', ''))
        self.rejected("UI_COMPONENT", card=CARD.replace('type="string"', 'type="lv_obj"'))
        self.rejected("UI_COMPONENT", card=CARD.replace('<view ', '<view extends="lv_slider" '))
        self.rejected("UI_COMPONENT_CYCLE", card=CARD.replace(
            '<lv_label name="title"', '<card name="again"/><lv_label name="title"'))

    def test_globals_only_declare_images(self):
        self.rejected("UI_SCHEMA", globals_xml='<globals><subjects><int name="x" value="0"/></subjects></globals>')
        self.rejected("UI_SCHEMA", globals_xml='<globals><fonts><bin name="f" src_path="f.bin"/></fonts></globals>')
        self.rejected("UI_ASSET_MISSING", screen=SCREEN.replace('src="logo"', 'src="other"'))

    def test_screen_structure(self):
        with self.assertRaises(UIError) as ctx:
            compile_package(self.root, "../demo")
        self.assertEqual(ctx.exception.code, "UI_SCREEN")
        with self.assertRaises(UIError) as ctx:
            compile_package(self.root, "missing")
        self.assertEqual(ctx.exception.code, "UI_FILE_MISSING")
        self.rejected("UI_SCREEN", screen=SCREEN.replace('<screen>', '<screen><consts><px name="gap" value="4"/></consts>'))
        self.rejected("UI_SCREEN", screen=SCREEN.replace('<screen>', '<screen permanent="true">'))

    def test_rejects_depth_node_and_size_limits(self):
        body = "".join(f'<lv_obj name="n{i}">' for i in range(7)) + '<lv_label name="last"/>' + "</lv_obj>" * 7
        self.rejected("UI_DEPTH", screen=f"<screen><view>{body}</view></screen>")
        labels = "".join(f'<lv_label name="n{i}"/>' for i in range(35))
        self.rejected("UI_NODE_LIMIT", screen=f"<screen><view>{labels}</view></screen>")
        self.rejected("UI_XML_SIZE", screen=SCREEN + " " * (16 * 1024))

    def test_rejects_unsafe_or_invalid_assets(self):
        for src, code in (("../../etc/passwd", "UI_ASSET_PATH"), ("https://example.invalid/x.png", "UI_ASSET_PATH"),
                          ("assets/missing.rgb565", "UI_ASSET_MISSING")):
            self.rejected(code, globals_xml=GLOBALS.replace("assets/logo.rgb565", src))
        (self.root / "assets/link.rgb565").symlink_to("logo.rgb565")
        self.rejected("UI_ASSET_PATH", globals_xml=GLOBALS.replace("assets/logo.rgb565", "assets/link.rgb565"))
        self.write(globals_xml=GLOBALS)
        (self.root / "assets/logo.rgb565").write_bytes(struct.pack("<HH", 33, 1) + b"\0" * 66)
        self.rejected("UI_ASSET_DIMENSIONS")
        (self.root / "assets/logo.rgb565").write_bytes(b"x" * (8 * 1024 + 1))
        self.rejected("UI_ASSET_SIZE")

    def test_rejects_representation_expansion_over_runtime_cap(self):
        files = "".join(f'<file name="i{i}" src_path="assets/i{i}.rgb565"/>' for i in range(4))
        for i in range(4):
            (self.root / f"assets/i{i}.rgb565").write_bytes(struct.pack("<HH", 31, 32) + b"\0" * (31 * 32 * 2))
        images = "".join(f'<lv_image name="i{i}" src="i{i}"/>' for i in range(4))
        self.rejected("UI_REPRESENTATION_SIZE", globals_xml=f"<globals><images>{files}</images></globals>",
                      screen=f"<screen><view>{images}</view></screen>")

    def test_update_is_typed_generation_scoped_and_bounded(self):
        update = {"generation": 4, "id": "status.title", "key": "title", "type": "string", "value": "Updated"}
        document = self.compile()
        validate_update(update, 4, True, document)
        for generation, active, code in ((3, True, "UI_UPDATE_STALE"), (4, False, "UI_UPDATE_STALE")):
            with self.assertRaises(UIError) as ctx:
                validate_update({**update, "generation": generation}, 4, active, document)
            self.assertEqual(ctx.exception.code, code)
        for change, code in (({"type": "pointer"}, "UI_UPDATE_TYPE"), ({"value": "x" * 81}, "UI_UPDATE_VALUE"),
                             ({"id": "caption"}, "UI_UPDATE_REFERENCE")):
            with self.assertRaises(UIError) as ctx:
                validate_update({**update, **change}, 4, True, document)
            self.assertEqual(ctx.exception.code, code)


if __name__ == "__main__":
    unittest.main()
