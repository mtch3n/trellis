package ui

import (
	"errors"
	"net/http"

	"github.com/mtch3n/trellis/internal/config"
)

// SettingInfo is one config key's metadata plus its current value, default
// and source in the global file: the shape GET and PATCH /api/settings both
// return, one per key in config.AllKeys order.
type SettingInfo struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Value       any      `json:"value"`
	Default     any      `json:"default"`
	Choices     []string `json:"choices,omitempty"`
	Min         *int     `json:"min,omitempty"`
	Source      string   `json:"source"`
	Editable    bool     `json:"editable"`
	Restart     bool     `json:"restart"`
	Description string   `json:"description"`
}

type settingsResponse struct {
	File     string        `json:"file"`
	Settings []SettingInfo `json:"settings"`
}

type settingsPatchResponse struct {
	File     string        `json:"file"`
	Settings []SettingInfo `json:"settings"`
	Restart  []string      `json:"restart"`
}

// settingInfos builds one SettingInfo per key config.Describe lists, reading
// cfg for the current value and present for whether the global file (rather
// than a built-in default) set it.
func settingInfos(cfg config.Config, present map[string]bool) []SettingInfo {
	defaults := config.Defaults()
	described := config.Describe()
	out := make([]SettingInfo, 0, len(described))
	for _, info := range described {
		value, _ := config.TypedValue(cfg, info.Key)
		def, _ := config.TypedValue(defaults, info.Key)
		source := "default"
		if present[info.Key] {
			source = "config"
		}
		out = append(out, SettingInfo{
			Key: info.Key, Type: string(info.Type), Value: value, Default: def,
			Choices: info.Choices, Min: info.Min, Source: source,
			Editable: info.Editable, Restart: info.Restart, Description: info.Description,
		})
	}
	return out
}

// handleGetSettings lists every setting in config.AllKeys order, grouped
// implicitly by that order's first dotted segment.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg, present, err := config.LoadWithPresence(s.root)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse{File: config.Path(s.root), Settings: settingInfos(cfg, present)})
}

type patchSettingsRequest struct {
	Set   map[string]any `json:"set"`
	Unset []string       `json:"unset"`
}

// handlePatchSettings validates and writes a batch of changes to
// config.yaml, calls the live-config hook so the daemon's Core picks up the
// settings it applies without a restart, and answers with GET's shape plus
// which of the changed keys still need one.
func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var in patchSettingsRequest
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	cfg, err := config.SetGlobalValues(s.root, in.Set, in.Unset)
	if err != nil {
		if ise, ok := errors.AsType[*config.InvalidSettingsError](err); ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":    "invalid settings",
				"code":     "invalid_settings",
				"problems": ise.Problems,
			})
			return
		}
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if s.liveConfig != nil {
		s.liveConfig(cfg)
	}

	_, present, err := config.LoadWithPresence(s.root)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}

	changed := make(map[string]bool, len(in.Set)+len(in.Unset))
	for key := range in.Set {
		changed[key] = true
	}
	for _, key := range in.Unset {
		changed[key] = true
	}
	restart := []string{}
	for _, info := range config.Describe() {
		if changed[info.Key] && info.Restart {
			restart = append(restart, info.Key)
		}
	}

	writeJSON(w, http.StatusOK, settingsPatchResponse{
		File: config.Path(s.root), Settings: settingInfos(cfg, present), Restart: restart,
	})
}
