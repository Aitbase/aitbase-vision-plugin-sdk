package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"
)

// AssistantTool is one entry of plugin.json's assistant_tools: a tool the voice assistant may call
// through plugins.call (D31 in the device app). The device checks the same rules and, when the
// block is invalid, offers none of the plugin's tools.
type AssistantTool struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Args        map[string]json.RawMessage `json:"args"`
}

// ToolArg is one argument's declaration: a string with max bytes, an int with min and max, or an
// enum of values.
type ToolArg struct {
	Type   string   `json:"type"`
	Max    *float64 `json:"max"`
	Min    *float64 `json:"min"`
	Values []string `json:"values"`
}

// labelOK is the device's label rule: one line of UTF-8 without control, bidirectional-override,
// zero-width or line separator characters.
func labelOK(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == 0x2028 || r == 0x2029:
			return false
		case (r >= 0x200b && r <= 0x200f) || r == 0x2060 || r == 0xfeff:
			return false
		case (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0x061c:
			return false
		}
	}
	return true
}

func whole(v *float64, lo, hi float64) bool {
	return v != nil && *v == math.Trunc(*v) && *v >= lo && *v <= hi
}

// onlyKeys reports whether the JSON object in data has no keys other than keys.
func onlyKeys(data []byte, keys ...string) bool {
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil {
		return false
	}
	for k := range obj {
		known := false
		for _, want := range keys {
			known = known || k == want
		}
		if !known {
			return false
		}
	}
	return true
}

func toolArgProblem(data []byte) string {
	var a ToolArg
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&a); err != nil {
		return "is not an object with a type"
	}
	switch a.Type {
	case "string":
		if a.Max != nil && !whole(a.Max, 1, float64(registryLimits.ToolStringMax)) {
			return fmt.Sprintf("max must be a whole number from 1 to %d", registryLimits.ToolStringMax)
		}
		if !onlyKeys(data, "type", "max") {
			return "a string takes only max"
		}
	case "int":
		if !whole(a.Min, math.MinInt32, math.MaxInt32) || !whole(a.Max, *a.Min, math.MaxInt32) {
			return "an int needs whole min and max, min <= max, within 32 bits"
		}
		if !onlyKeys(data, "type", "min", "max") {
			return "an int takes only min and max"
		}
	case "enum":
		if len(a.Values) < 1 || len(a.Values) > registryLimits.ToolEnumValuesMax {
			return fmt.Sprintf("an enum needs 1 to %d values", registryLimits.ToolEnumValuesMax)
		}
		seen := map[string]bool{}
		for _, v := range a.Values {
			if v == "" || len(v) > 32 || !labelOK(v) || seen[v] {
				return fmt.Sprintf("enum value %q must be unique, one line, at most 32 bytes", v)
			}
			seen[v] = true
		}
		if !onlyKeys(data, "type", "values") {
			return "an enum takes only values"
		}
	default:
		return fmt.Sprintf("type %q is not string, int or enum", a.Type)
	}
	return ""
}

// checkAssistantTools validates the assistant_tools block (raw is nil when it is absent).
func checkAssistantTools(r *Result, file string, raw json.RawMessage, requested bool) {
	if raw == nil {
		return
	}
	p := file + "#assistant_tools"
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || len(items) > registryLimits.ToolsMax {
		r.add(Error, "TOOLS_FORMAT", p, fmt.Sprintf("assistant_tools must be a list of at most %d tools", registryLimits.ToolsMax),
			`for example [{"name": "today", "description": "Today's weather for a city", "args": {"city": {"type": "string", "max": 40}}}]`)
		return
	}
	if len(items) > 0 && !requested {
		r.add(Error, "TOOLS_GRANT_MISSING", file+"#requested_capabilities", "the plugin declares assistant_tools but does not request \"assistant.tools\"",
			`add "assistant.tools" to requested_capabilities`)
	}
	seen := map[string]bool{}
	for _, item := range items {
		var t AssistantTool
		if err := json.Unmarshal(item, &t); err != nil || !onlyKeys(item, "name", "description", "args") {
			r.add(Error, "TOOLS_FORMAT", p, "each tool is an object with only name, description and args", "remove other fields")
			continue
		}
		switch {
		case !namePattern.MatchString(t.Name) || len(t.Name) > registryLimits.ToolNameMax:
			r.add(Error, "TOOL_NAME", p, fmt.Sprintf("tool name %q is not valid", t.Name),
				fmt.Sprintf("use lowercase letters, digits and '_', starting with a letter, at most %d characters", registryLimits.ToolNameMax))
		case seen[t.Name]:
			r.add(Error, "TOOL_NAME", p, fmt.Sprintf("tool %q is declared twice", t.Name), "declare each tool once")
		}
		seen[t.Name] = true
		if t.Description == "" || len(t.Description) > registryLimits.ToolDescriptionMax || !labelOK(t.Description) {
			r.add(Error, "TOOL_DESCRIPTION", p+"."+t.Name, fmt.Sprintf("tool %q needs a description of one line, at most %d bytes", t.Name, registryLimits.ToolDescriptionMax),
				"say in a few words what the tool answers; the assistant reads it to choose the tool")
		}
		if len(t.Args) > registryLimits.ToolArgsMax {
			r.add(Error, "TOOL_ARG", p+"."+t.Name, fmt.Sprintf("tool %q has more than %d arguments", t.Name, registryLimits.ToolArgsMax), "remove some")
		}
		for name, spec := range t.Args {
			problem := toolArgProblem(spec)
			if !namePattern.MatchString(name) || len(name) > registryLimits.ToolNameMax {
				problem = "the name must be lowercase letters, digits and '_', starting with a letter"
			}
			if problem != "" {
				r.add(Error, "TOOL_ARG", p+"."+t.Name+".args."+name, fmt.Sprintf("argument %q of tool %q: %s", name, t.Name, problem),
					`use {"type": "string", "max": 40}, {"type": "int", "min": 1, "max": 7} or {"type": "enum", "values": ["c", "f"]}`)
			}
		}
	}
}
