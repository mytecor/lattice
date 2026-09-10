package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ruleEnvelope is the minimal decoder view of any routing rule: it carries
// only the named route selector and the action discriminator, so the decoder
// can choose the concrete type before strict field validation.
type ruleEnvelope struct {
	Route  string `json:"route"`
	Action string `json:"action"`
}

// RoutingRules is the external rule list. UnmarshalJSON performs the
// two-phase strict decode: the envelope pins the action, then the same object
// is decoded into the concrete rule type with DisallowUnknownFields. Unknown
// actions, unknown fields, fields owned by another action and the removed
// legacy `match`/`map.providers` contracts are rejected here; every error
// carries the rule index and, when known, the named route and the action.
type RoutingRules []Rule

func (rs *RoutingRules) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	decoded := make(RoutingRules, 0, len(raw))
	for index, item := range raw {
		rule, err := decodeRule(item, index)
		if err != nil {
			return err
		}
		decoded = append(decoded, rule)
	}
	*rs = decoded
	return nil
}

// decodeRule decodes one rule object in two phases: a minimal envelope
// selects the concrete type from ruleRegistry, then the same object is
// decoded into that type with DisallowUnknownFields so a field owned by
// another action is rejected on the decode boundary.
func decodeRule(data []byte, index int) (Rule, error) {
	var envelope ruleEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("routing rule %d: %w", index, err)
	}
	route := strings.TrimSpace(envelope.Route)
	action := strings.ToLower(strings.TrimSpace(envelope.Action))
	if route == "" {
		return nil, fmt.Errorf("routing rule %d (action %q) has no route", index, action)
	}
	if action == "" {
		return nil, fmt.Errorf("routing rule %d (route %q) has no action", index, route)
	}
	descriptor, ok := ruleRegistry[action]
	if !ok {
		return nil, fmt.Errorf("routing rule %d (route %q) has unsupported action %q", index, route, action)
	}
	rule := descriptor.new()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(rule); err != nil {
		return nil, fmt.Errorf("routing rule %d (route %q, action %q): %w", index, route, action, err)
	}
	return rule, nil
}
