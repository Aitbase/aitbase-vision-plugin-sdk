package check

import (
	"encoding/json"
	"fmt"
)

// The manifest's settings (device decision D38): values the owner fills in on the Device Console,
// which the plugin reads from its storage as "settings.<key>".
type setting struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Placeholder string `json:"placeholder"`
}

// checkSettings validates the settings block (raw is nil when it is absent). The device refuses an
// invalid block as a whole: the plugin runs without settings.
func checkSettings(r *Result, file string, raw json.RawMessage) {
	if raw == nil {
		return
	}
	p := file + "#settings"
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || len(items) > registryLimits.SettingsMax {
		r.add(Error, "SETTINGS_FORMAT", p, fmt.Sprintf("settings must be a list of at most %d fields", registryLimits.SettingsMax),
			`for example [{"key": "ha_url", "label": "Home Assistant address", "type": "url"}]`)
		return
	}
	seen := map[string]bool{}
	for _, item := range items {
		var s setting
		if err := json.Unmarshal(item, &s); err != nil || !onlyKeys(item, "key", "label", "type", "placeholder") {
			r.add(Error, "SETTINGS_FORMAT", p, "each field is an object with only key, label, type and placeholder", "remove other fields")
			continue
		}
		problem := ""
		switch {
		case !namePattern.MatchString(s.Key) || len(s.Key) > registryLimits.SettingKeyMax:
			problem = fmt.Sprintf("key %q must be lowercase letters, digits and '_', starting with a letter, at most %d characters", s.Key, registryLimits.SettingKeyMax)
		case seen[s.Key]:
			problem = fmt.Sprintf("key %q is declared twice", s.Key)
		case s.Label == "" || len(s.Label) > registryLimits.SettingLabelMax || !labelOK(s.Label):
			problem = fmt.Sprintf("field %q needs a label of one line, at most %d bytes", s.Key, registryLimits.SettingLabelMax)
		case s.Type != "text" && s.Type != "url" && s.Type != "secret":
			problem = fmt.Sprintf("field %q has type %q; use text, url or secret", s.Key, s.Type)
		case len(s.Placeholder) > registryLimits.SettingLabelMax || !labelOK(s.Placeholder):
			problem = fmt.Sprintf("field %q: the placeholder is one line of at most %d bytes", s.Key, registryLimits.SettingLabelMax)
		}
		seen[s.Key] = true
		if problem != "" {
			r.add(Error, "SETTING_FIELD", p+"."+s.Key, problem, "the device ignores the whole settings block when one field is invalid")
		}
	}
}
