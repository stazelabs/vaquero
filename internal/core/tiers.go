package core

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// Tiers are a ticket's execution tiers, lightest work last but design
// first: design is spec and plan work, the rest how much a build needs.
// A ticket's tier may also be "" (none).
var Tiers = []string{"design", "light", "standard", "heavy"}

// MaxModel bounds a ticket's model and each model in a tier map;
// MaxHarnesses the harnesses in a tier map.
const (
	MaxModel     = 80
	MaxHarnesses = 20
)

const (
	tierMsg    = "Choose design, light, standard or heavy."
	tierMapMsg = "Enter the tier map as JSON: each harness names a model for any of design, light, standard and heavy."
)

// TierMap turns a tier into a model for each harness: harness → tier →
// model. A project stores "" for DefaultTierMap.
type TierMap map[string]map[string]string

// DefaultTierMap is the shipped map. Each call returns a fresh copy. The
// design tier takes the heavy model.
//
// The codex ids are from the Codex models page
// (https://developers.openai.com/codex/models, 2026-11-25): Astra "for
// demanding projects", Sol "for everyday coding", Luna "for focused,
// high-volume tasks".
func DefaultTierMap() TierMap {
	return TierMap{
		"claude": {"design": "claude-opus-5-5", "light": "claude-haiku-5-5", "standard": "claude-sonnet-5-5", "heavy": "claude-opus-5-5"},
		"codex":  {"design": "gpt-6.2-astra", "light": "gpt-6-luna", "standard": "gpt-6.2-sol", "heavy": "gpt-6.2-astra"},
	}
}

// Model is the model m names for harness at tier, "" for none.
func (m TierMap) Model(harness, tier string) string { return m[harness][tier] }

// String is m as compact JSON with its keys sorted, as a project stores it.
func (m TierMap) String() string {
	b, _ := json.Marshal(m) // a map of strings always marshals
	return string(b)
}

// Indent is m as indented JSON with its keys sorted, for editing.
func (m TierMap) Indent() string {
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}

// ParseTierMap reads a tier map from JSON; "" (or only spaces) is the
// default. A refusal is an *Invalid on tier_map.
func ParseTierMap(s string) (TierMap, error) {
	if strings.TrimSpace(s) == "" {
		return DefaultTierMap(), nil
	}
	dec := json.NewDecoder(strings.NewReader(s))
	var m TierMap
	if err := dec.Decode(&m); err != nil || m == nil || dec.More() {
		return nil, invalid("tier_map", tierMapMsg)
	}
	if len(m) > MaxHarnesses {
		return nil, invalid("tier_map", "Keep the tier map to 20 harnesses or fewer.")
	}
	for _, h := range slices.Sorted(maps.Keys(m)) {
		if strings.TrimSpace(h) == "" || h != strings.TrimSpace(h) {
			return nil, invalid("tier_map", "Name each harness.")
		}
		if hasControl(h) {
			return nil, invalid("tier_map", controlMsg)
		}
		if len([]rune(h)) > 40 {
			return nil, invalid("tier_map", "Keep each harness name under 40 characters.")
		}
		if m[h] == nil {
			m[h] = map[string]string{}
		}
		for tier, model := range m[h] {
			if !slices.Contains(Tiers, tier) {
				return nil, invalid("tier_map", "Use only the tiers design, light, standard and heavy.")
			}
			model = strings.TrimSpace(model)
			if len([]rune(model)) > MaxModel || hasControl(model) {
				return nil, invalid("tier_map", "Keep each model under 80 characters.")
			}
			m[h][tier] = model
		}
	}
	return m, nil
}

// EffectiveTierMap is a project's stored map, or the default when it
// stores none (or, never expected, one that no longer parses).
func EffectiveTierMap(stored string) TierMap {
	m, err := ParseTierMap(stored)
	if err != nil {
		return DefaultTierMap()
	}
	return m
}

// storedTierMap is s as a project stores it: "" for the default, else
// compact JSON with sorted keys.
func storedTierMap(s string) (string, error) {
	m, err := ParseTierMap(s)
	if err != nil {
		return "", err
	}
	if out := m.String(); out != DefaultTierMap().String() {
		return out, nil
	}
	return "", nil
}

// checkTier validates a ticket's tier and model into f.
func checkTier(f fields, tier, model string) (string, string) {
	tier = strings.TrimSpace(tier)
	if tier != "" && !slices.Contains(Tiers, tier) {
		f.add("tier", tierMsg)
	}
	model = strings.TrimSpace(model)
	if hasControl(model) {
		f.add("model", controlMsg)
	} else if len([]rune(model)) > MaxModel {
		f.add("model", "Keep the model under 80 characters.")
	}
	return tier, model
}
