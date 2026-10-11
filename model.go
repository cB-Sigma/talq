// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package talq

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Device is a TALQ device as exchanged on the API.
type Device struct {
	Address   string           `json:"address"`
	Name      string           `json:"name"`
	Class     string           `json:"class"`
	Functions []map[string]any `json:"functions,omitempty"`
}

// Ref is a TALQ ResourceReference.
type Ref struct {
	Resource string `json:"resource"`
	Address  string `json:"address"`
}

// AssetKinds are the gateway API's asset type collections.
var AssetKinds = []string{"lamp-types", "luminaire-types", "driver-types", "controller-types", "bracket-types"}

// AssetAttrs maps the device attributes that refer to an asset type to the
// type's collection and a short key for it.
var AssetAttrs = map[string][2]string{
	"lampTypeId":            {"lamp-types", "lampType"},
	"luminaireTypeAddress":  {"luminaire-types", "luminaireType"},
	"driverTypeAddress":     {"driver-types", "driverType"},
	"controllerTypeAddress": {"controller-types", "controllerType"},
	"bracketTypeAddress":    {"bracket-types", "bracketType"},
}

// GroupPurposes are the purposes a group may have.
var GroupPurposes = []string{"unknown", "override", "calendar", "loggerConfig", "lampType", "attributes"}

// IsUUID reports whether s is a UUID in its 36-character text form, which
// TALQ uses for addresses.
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// AttrValue flattens a TALQ attribute object to its value, or a state's
// value for the simple state kinds, with its timestamp (zero without one).
func AttrValue(a map[string]any) (any, time.Time) {
	var at time.Time
	if ts, _ := a["timestamp"].(string); ts != "" {
		at, _ = time.Parse(time.RFC3339Nano, ts)
	}
	v := a["value"]
	if st, ok := v.(map[string]any); ok {
		switch st["name"] {
		case "LevelState", "IntegerState", "FloatState", "BooleanState", "TextState":
			return st["value"], at
		}
	}
	return v, at
}

// LevelCommand is a target command setting a light level (0 to 100).
func LevelCommand(level int) map[string]any {
	return map[string]any{"state": map[string]any{"name": "LevelState", "value": level}}
}

// OverrideTarget is an override command's targetCommand: a whole level
// from 0 to 100, an optional RFC 3339 expiration and ramp time, and the
// CMS's reference.
func OverrideTarget(lv float64, expiration string, ramp *float64, ref string) (map[string]any, error) {
	if lv < 0 || lv > 100 || lv != float64(int(lv)) {
		return nil, fmt.Errorf("level %v is not a whole number from 0 to 100", lv)
	}
	tc := map[string]any{"state": map[string]any{"name": "LevelState", "value": int(lv)}, "reason": "override", "cmsRefId": ref}
	if expiration != "" {
		if _, err := time.Parse(time.RFC3339, expiration); err != nil {
			return nil, fmt.Errorf("expiration %q is not an RFC 3339 time", expiration)
		}
		tc["expiration"] = expiration
	}
	if ramp != nil {
		if *ramp < 0 {
			return nil, fmt.Errorf("rampTime %v is not a time of 0 seconds or more", *ramp)
		}
		tc["rampToLevelTime"] = *ramp
	}
	return tc, nil
}

// MajorMinor is the "major.minor" of a TALQ API version.
func MajorMinor(v string) string {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return parts[0]
}

// SourceDevice splits a log record's srcAddress ("{device}/{function}",
// optionally with a leading "/devices/") into device (lower case) and
// function.
func SourceDevice(src string) (dev, fn string) {
	src = strings.TrimPrefix(strings.TrimPrefix(src, "/"), "devices/")
	dev, fn, _ = strings.Cut(src, "/")
	if i := strings.IndexByte(fn, '/'); i >= 0 {
		fn = fn[:i]
	}
	return strings.ToLower(dev), fn
}

// WriteError answers with a TALQ error body: a list of one
// TALQErrorMessage.
func WriteError(w http.ResponseWriter, status int, key, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode([]ErrorMessage{{Key: key, Description: desc}})
}
