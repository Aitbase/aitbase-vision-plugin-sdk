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
        self.rejected("UI_WIDGET", screen=SCREEN.replace('<lv_image name="logo" src="logo"/>', '<lv_textarea name="s"/>'))
        self.rejected("UI_WIDGET", screen=SCREEN.replace('<lv_image name="logo" src="logo"/>', '<Card name="s"/>'))
        self.rejected("UI_PROPERTY", screen=SCREEN.replace('width="240"', 'hidden="true" width="240"'))
        self.rejected("UI_PROPERTY", screen=SCREEN.replace('width="240"', 'style_shadow_width="4" width="240"'))
        self.rejected("UI_PROPERTY", screen=SCREEN.replace('width="240"', 'hidden="true" width="240"'))
        self.rejected("UI_PROPERTY_VALUE", screen=SCREEN.replace('width="240"', 'width="-1"'))
        self.rejected("UI_PROPERTY_VALUE", screen=SCREEN.replace('width="240"', 'width="50%"'))

    def test_colors_are_hex_or_platform_tokens(self):
        # D36: any #rrggbb or 0xRRGGBB, or a token; nothing else.
        for color, expected in (('"#12AB9f"', "#12ab9f"), ('"0x123456"', "#123456"), ('"#accent"', "#00e5ff")):
            self.write(screen=SCREEN.replace('"#surface"', color, 1))
            self.assertEqual(self.compile()["tree"]["props"]["background"], expected)
        self.write(screen=SCREEN)
        self.rejected("UI_STYLE", screen=SCREEN.replace('"#surface"', '"#brand"', 1))
        self.rejected("UI_STYLE", screen=SCREEN.replace('"#surface"', '"#12345"', 1))
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
        self.rejected("UI_COMPONENT", card=CARD.replace('<view ', '<view extends="lv_textarea" '))
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
        labels = "".join(f'<lv_label name="n{i}"/>' for i in range(64))
        self.rejected("UI_NODE_LIMIT", screen=f"<screen><view>{labels}</view></screen>")
        self.rejected("UI_XML_SIZE", screen=SCREEN + " " * (24 * 1024))

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
        # Styled labels on top: the XML fits, the compiled document doesn't (24 KiB, D36).
        style = 'x="100" y="100" width="100" height="100" style_pad_all="10" style_radius="10" style_opa="200"'
        labels = "".join(f'<lv_label name="l{i}" text="{"x" * 80}" {style}/>' for i in range(58))
        self.rejected("UI_REPRESENTATION_SIZE", globals_xml=f"<globals><images>{files}</images></globals>",
                      screen=f"<screen><view>{images}{labels}</view></screen>")

    def test_d36_display_widgets_compile(self):
        widgets = (
            '<lv_bar name="cpu" value="40" min_value="0" max_value="100" style_bg_color-indicator="#00ff00" bind_value="value.1"/>'
            '<lv_arc name="gauge" value="10" style_arc_width="6" rotation="90" bg_start_angle="0" bg_end_angle="270" bind_value="face.2"/>'
            '<lv_led name="dot" brightness="120" color="0xff0000"/>'
            '<lv_spinner name="wait" anim_duration="1000" arc_length="120"/>'
            '<lv_line name="trace" points="0,0 40,20 80,0" style_line_color="#ffffff"/>'
            '<lv_scale name="ruler" mode="round_inner" min_value="0" max_value="50" total_tick_count="11" major_tick_every="5"/>'
            '<lv_chart name="history" type="bar" point_count="8" min_value="0" max_value="10">'
            '<lv_chart-series values="1 2 3" bind_values="value.4"/><lv_chart-series bind_values="value.5"/></lv_chart>'
            '<lv_table name="grid" row_count="2" column_count="2" style_text_font="16" style_text_align="center">'
            '<lv_table-cell row="0" column="1" value="b"/><lv_table-row row="1" bind_value="value.6"/></lv_table>'
            '<lv_obj name="box" flex_flow="row_wrap" style_flex_main_place="space_between" style_pad_row="4" '
            'style_border_width="2" style_border_color="#control" style_bg_opa="128"/>')
        self.write(screen=f"<screen><view>{widgets}</view></screen>")
        tree = self.compile()["tree"]
        nodes = {node["id"]: node for node in tree["children"]}
        self.assertEqual(nodes["cpu"]["props"], {"value": 40, "min": 0, "max": 100, "indicator_color": "#00ff00"})
        self.assertEqual(nodes["cpu"]["binding"], {"property": "value", "key": "value.1", "type": "string"})
        self.assertEqual(nodes["gauge"]["props"]["thickness"], 6)
        self.assertEqual(nodes["dot"]["props"], {"brightness": 120, "indicator_color": "#ff0000"})
        self.assertEqual(nodes["wait"]["props"], {"duration": 1000, "arc_length": 120})
        self.assertEqual(nodes["trace"]["props"]["points"], [0, 0, 40, 20, 80, 0])
        self.assertEqual(nodes["ruler"]["props"]["scale_mode"], "round_inner")
        self.assertEqual(nodes["history"]["props"]["series_count"], 2)
        self.assertEqual(nodes["history"]["props"]["series_1"], [1, 2, 3])
        self.assertEqual([b["property"] for b in nodes["history"]["bindings"]], ["series_1", "series_2"])
        self.assertEqual(nodes["grid"]["props"]["cells"], ["", "b"])
        self.assertEqual(nodes["grid"]["binding"]["property"], "row_1")
        self.assertEqual(nodes["box"]["props"]["flex_flow"], "row_wrap")
        self.assertEqual(nodes["box"]["props"]["bg_opa"], 128)

    def test_placement_and_label_attributes_compile(self):
        widgets = (
            '<lv_obj name="gauge" width="72" height="72">'
            '<lv_arc name="ring" width="72" height="72" style_arc_color="#3a3a3a" style_arc_color-indicator="#ff6a00"/>'
            '<lv_label name="pct" floating="true" align="center" text="45%"/></lv_obj>'
            '<lv_obj name="row" flex_flow="row"><lv_bar name="fill" width="1" flex_grow="1"/>'
            '<lv_label name="cpu" width="80" long_mode="scroll_circular" text="AMD Ryzen 9"/></lv_obj>')
        self.write(screen=f"<screen><view>{widgets}</view></screen>")
        tree = self.compile()["tree"]
        gauge, row = tree["children"]
        self.assertEqual(gauge["children"][0]["props"]["track_color"], "#3a3a3a")
        self.assertEqual(gauge["children"][1]["props"], {"floating": 1, "align": "center", "text": "45%"})
        self.assertEqual(row["children"][0]["props"]["flex_grow"], 1)
        self.assertEqual(row["children"][1]["props"]["long_mode"], "scroll_circular")

    def test_d36_bounds_are_checked_at_compile_time(self):
        cases = (
            ('<lv_arc name="a" style_arc_width="17"/>', "UI_PROPERTY_VALUE"),
            ('<lv_bar name="b" min_value="10" max_value="10"/>', "UI_PROPERTY_VALUE"),
            ('<lv_line name="l" points="0,0 1"/>', "UI_PROPERTY_VALUE"),
            ('<lv_line name="l" points="' + " ".join("0,0" for _ in range(17)) + '"/>', "UI_PROPERTY_VALUE"),
            ('<lv_scale name="s" total_tick_count="5" major_tick_every="6"/>', "UI_PROPERTY_VALUE"),
            ('<lv_chart name="c" point_count="2"><lv_chart-series values="1 2 3"/></lv_chart>', "UI_PROPERTY_VALUE"),
            ('<lv_chart name="c"><lv_chart-series/><lv_chart-series/><lv_chart-series/></lv_chart>', "UI_PROPERTY_VALUE"),
            ('<lv_table name="t" row_count="1"><lv_table-cell row="1" column="0" value="x"/></lv_table>', "UI_PROPERTY_VALUE"),
            ('<lv_table name="t"><lv_table-cell row="0" column="0" value="' + "x" * 33 + '"/></lv_table>', "UI_TEXT_SIZE"),
            ('<lv_label name="x" style_text_font="15"/>', "UI_PROPERTY_VALUE"),
            ('<lv_label name="x" style_text_align="justify"/>', "UI_PROPERTY_VALUE"),
            ('<lv_label name="x" value="3"/>', "UI_PROPERTY"),
            ('<lv_bar name="b" bind_value="secret"/>', "UI_BINDING"),
            ('<lv_obj name="x" flex_flow="grid"/>', "UI_PROPERTY_VALUE"),
            ('<lv_label name="x" style_pad_all="4" style_pad_all2="4"/>', "UI_PROPERTY"),
            ('<lv_spinner name="a"/><lv_spinner name="b"/><lv_spinner name="c"/>', "UI_WIDGET_LIMIT"),
            ('<lv_label name="x" style_text_color="#ffffff" style_line_color="#000000"/>', "UI_PROPERTY"),
            ('<lv_label name="x" long_mode="marquee"/>', "UI_PROPERTY_VALUE"),
            ('<lv_bar name="x" long_mode="clip"/>', "UI_PROPERTY"),
            ('<lv_label name="x" align="middle"/>', "UI_PROPERTY_VALUE"),
            ('<lv_label name="x" floating="yes"/>', "UI_PROPERTY_VALUE"),
            ('<lv_bar name="x" flex_grow="9"/>', "UI_PROPERTY_VALUE"),
            ('<lv_bar name="x" style_arc_color="#3a3a3a"/>', "UI_PROPERTY"),
        )
        for xml, code in cases:
            with self.subTest(xml=xml):
                self.rejected(code, screen=f"<screen><view>{xml}</view></screen>")

    def test_d36_input_widgets_compile(self):
        widgets = (
            '<lv_checkbox name="agree" text="I agree" checked="true" bind_checked="value.1"/>'
            '<lv_switch name="dark" style_bg_color-indicator="#00ff00"/>'
            '<lv_slider name="vol" min_value="0" max_value="10" step="2" value="4" bind_value="value.2"/>'
            '<lv_roller name="city" options="Hanoi\\nHue\\nSaigon" selected="1"/>'
            '<lv_dropdown name="unit" options="C\\nF" bind_value="value.3"/>')
        self.write(screen=f"<screen><view>{widgets}</view></screen>")
        nodes = {node["id"]: node for node in self.compile()["tree"]["children"]}
        self.assertEqual(nodes["agree"]["type"], "checkbox")
        self.assertEqual(nodes["agree"]["props"], {"text": "I agree", "checked": 1})
        self.assertEqual(nodes["agree"]["binding"], {"property": "checked", "key": "value.1", "type": "string"})
        self.assertEqual(nodes["dark"]["props"], {"indicator_color": "#00ff00"})
        self.assertEqual(nodes["vol"]["props"], {"min": 0, "max": 10, "step": 2, "value": 4})
        self.assertEqual(nodes["vol"]["binding"]["property"], "value")
        self.assertEqual(nodes["city"]["props"], {"options": ["Hanoi", "Hue", "Saigon"], "selected": 1})
        self.assertEqual(nodes["unit"]["binding"]["property"], "selected")
        self.assertNotIn("action", nodes["vol"])

    def test_d36_input_bounds(self):
        cases = (
            ('<lv_roller name="r"/>', "UI_PROPERTY_VALUE"),
            ('<lv_roller name="r" options="' + "\\n".join(str(i) for i in range(9)) + '"/>', "UI_PROPERTY_VALUE"),
            ('<lv_dropdown name="d" options="' + "x" * 25 + '"/>', "UI_PROPERTY_VALUE"),
            ('<lv_dropdown name="d" options="a\\n\\nb"/>', "UI_PROPERTY_VALUE"),
            ('<lv_roller name="r" options="a\\nb" selected="2"/>', "UI_PROPERTY_VALUE"),
            ('<lv_slider name="s" step="0"/>', "UI_PROPERTY_VALUE"),
            ('<lv_checkbox name="c" checked="yes"/>', "UI_PROPERTY_VALUE"),
            ('<lv_switch name="c" text="a"/>', "UI_PROPERTY"),
            ('<lv_switch name="c" bind_value="value.1"/>', "UI_PROPERTY"),
            ('<lv_slider name="s" bind_checked="value.1"/>', "UI_PROPERTY"),
            ('<lv_slider name="s"><event_cb trigger="clicked" callback="aitbase_action" user_data="template.ok"/></lv_slider>',
             "UI_EVENT"),
        )
        for widget, code in cases:
            with self.subTest(widget=widget):
                self.rejected(code, screen=f"<screen><view>{widget}</view></screen>")

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
