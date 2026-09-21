package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// nft -j -n emits libnftables-json objects, with numeric protocol values and
// priorities. Only the exact owned ruleset is evidence: unrecognised objects,
// table flags, chain hooks and additional statements cannot widen its scope.
func exitGuardRulesObserved(raw []byte, table, device string, mark uint32, tunnel bool, family api.ExitFamilyMode) bool {
	if tunnel && !exitGuardFamilyValid(family) {
		return false
	}
	objects, ok := exitGuardObjects(raw)
	if !ok {
		return false
	}
	tables := 0
	chains := map[string]bool{}
	var rules []any
	var forwarding []any
	for _, object := range objects {
		if len(object) != 1 {
			return false
		}
		for kind, value := range object {
			body, ok := value.(map[string]any)
			if !ok || !exitGuardIgnoreHandle(body) {
				return false
			}
			switch kind {
			case "table":
				if !exitGuardEmptyFlags(body) || !reflect.DeepEqual(body, map[string]any{"family": "inet", "name": table}) {
					return false
				}
				tables++
			case "chain":
				name, ok := body["name"].(string)
				if !ok || (name != "output" && name != "forward") || chains[name] {
					return false
				}
				want := map[string]any{"family": "inet", "table": table, "name": name, "type": "filter", "hook": name, "prio": json.Number("0"), "policy": "drop"}
				if !reflect.DeepEqual(body, want) {
					return false
				}
				chains[name] = true
			case "rule":
				if len(body) != 4 || body["family"] != "inet" || body["table"] != table || (body["chain"] != "output" && body["chain"] != "forward") {
					return false
				}
				if body["chain"] == "output" {
					rules = append(rules, body["expr"])
				} else {
					forwarding = append(forwarding, body["expr"])
				}
			default:
				return false
			}
		}
	}
	if tables != 1 || len(chains) != 2 {
		return false
	}
	if len(rules) >= 3 {
		var ok bool
		rules[2], ok = exitGuardNDDependencies(rules[2])
		if !ok {
			return false
		}
	}
	// Sets have deterministic numeric ordering with -n. Require exact match
	// operators and statement order, including the terminal accept verdict.
	wantJSON := fmt.Sprintf(`[
[{"match":{"op":"==","left":{"meta":{"key":"oifname"}},"right":"lo"}},{"accept":null}],
[{"match":{"op":"==","left":{"meta":{"key":"mark"}},"right":%d}},{"match":{"op":"==","left":{"meta":{"key":"l4proto"}},"right":{"set":[6,17]}}},{"accept":null}],
[{"match":{"op":"!=","left":{"meta":{"key":"oifname"}},"right":%q}},{"match":{"op":"==","left":{"payload":{"protocol":"ip6","field":"hoplimit"}},"right":255}},{"match":{"op":"==","left":{"payload":{"protocol":"icmpv6","field":"type"}},"right":{"set":[133,135,136]}}},{"match":{"op":"==","left":{"payload":{"protocol":"icmpv6","field":"code"}},"right":0}},{"accept":null}]
]`, mark, device)
	decoder := json.NewDecoder(bytes.NewBufferString(wantJSON))
	decoder.UseNumber()
	var want []any
	if decoder.Decode(&want) != nil {
		return false
	}
	if tunnel {
		selected, ordinary := exitGuardFamilyProtocols(family)
		tunRule := []any{map[string]any{"match": map[string]any{"op": "==", "left": map[string]any{"meta": map[string]any{"key": "oifname"}}, "right": device}}, map[string]any{"accept": nil}}
		if ordinary != "" {
			ordinaryRule := []any{exitGuardNFProtoMatch(ordinary), map[string]any{"accept": nil}}
			if !reflect.DeepEqual(forwarding, []any{ordinaryRule}) {
				return false
			}
			want = append(want, ordinaryRule)
			tunRule = append([]any{exitGuardNFProtoMatch(selected)}, tunRule...)
		} else if len(forwarding) != 0 {
			return false
		}
		want = append(want, tunRule)
	} else if len(forwarding) != 0 {
		return false
	}
	return reflect.DeepEqual(rules, want)
}

func exitGuardNFProtoMatch(protocol string) any {
	number := json.Number("2")
	if protocol == "ipv6" {
		number = json.Number("10")
	}
	return map[string]any{"match": map[string]any{"op": "==", "left": map[string]any{"meta": map[string]any{"key": "nfproto"}}, "right": number}}
}

// Querying all tables avoids interpreting a missing-table command error as
// absence. Unrelated, well-formed tables are allowed; malformed output is not.
func exitGuardTableAbsent(raw []byte, owned string) bool {
	objects, ok := exitGuardObjects(raw)
	if !ok {
		return false
	}
	for _, object := range objects {
		if len(object) != 1 {
			return false
		}
		body, ok := object["table"].(map[string]any)
		if !ok || !exitGuardIgnoreHandle(body) {
			return false
		}
		name, nameOK := body["name"].(string)
		family, familyOK := body["family"].(string)
		if !nameOK || name == "" || !familyOK {
			return false
		}
		switch family {
		case "ip", "ip6", "inet", "arp", "bridge", "netdev":
		default:
			return false
		}
		if family == "inet" && name == owned {
			return false
		}
		if comment, exists := body["comment"]; exists {
			if _, ok := comment.(string); !ok {
				return false
			}
			delete(body, "comment")
		}
		// Flags of an unrelated table do not affect owned-table absence, but
		// still validate their schema rather than accepting arbitrary objects.
		if flags, exists := body["flags"]; exists {
			values, ok := flags.([]any)
			if !ok {
				flag, scalar := flags.(string)
				if !scalar {
					return false
				}
				values = []any{flag}
			}
			for _, flag := range values {
				if flag != "dormant" && flag != "owner" && flag != "persist" {
					return false
				}
			}
			delete(body, "flags")
		}
		if len(body) != 2 {
			return false
		}
	}
	return true
}

// nft may retain these generated protocol prerequisites in inet ND rules.
// Each is an exact additional restriction; no other statements may be ignored.
// In particular ip6 nexthdr=58 is stronger than l4proto=58 (extension headers
// do not match). Combined native output still requires platform qualification.
func exitGuardNDDependencies(value any) (any, bool) {
	statements, ok := value.([]any)
	if !ok {
		return nil, false
	}
	seen := map[string]bool{}
	filtered := make([]any, 0, len(statements))
	for i, statement := range statements {
		guard := ""
		for _, dependency := range []struct {
			key   string
			left  any
			right json.Number
		}{
			{"nfproto", map[string]any{"meta": map[string]any{"key": "nfproto"}}, json.Number("10")},
			{"l4proto", map[string]any{"meta": map[string]any{"key": "l4proto"}}, json.Number("58")},
			{"nexthdr", map[string]any{"payload": map[string]any{"protocol": "ip6", "field": "nexthdr"}}, json.Number("58")},
		} {
			if reflect.DeepEqual(statement, map[string]any{"match": map[string]any{"op": "==", "left": dependency.left, "right": dependency.right}}) {
				guard = dependency.key
				break
			}
		}
		if guard != "" {
			if seen[guard] || i == len(statements)-1 {
				return nil, false
			}
			seen[guard] = true
			continue
		}
		filtered = append(filtered, statement)
	}
	return filtered, true
}

func exitGuardEmptyFlags(body map[string]any) bool {
	flags, exists := body["flags"]
	if !exists {
		return true
	}
	values, ok := flags.([]any)
	if !ok || len(values) != 0 {
		return false
	}
	delete(body, "flags")
	return true
}

func exitGuardIgnoreHandle(body map[string]any) bool {
	handle, exists := body["handle"]
	if !exists {
		return true
	}
	number, ok := handle.(json.Number)
	if !ok {
		return false
	}
	value, err := strconv.ParseUint(string(number), 10, 64)
	if err != nil || value == 0 {
		return false
	}
	delete(body, "handle")
	return true
}

func exitGuardObjects(raw []byte) ([]map[string]any, bool) {
	if len(raw) > 1<<20 {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, ok := exitGuardJSONValue(decoder, 0)
	if !ok {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	root, ok := value.(map[string]any)
	if !ok || len(root) != 1 {
		return nil, false
	}
	array, ok := root["nftables"].([]any)
	if !ok || len(array) == 0 {
		return nil, false
	}
	metaObject, ok := array[0].(map[string]any)
	if !ok || len(metaObject) != 1 {
		return nil, false
	}
	meta, ok := metaObject["metainfo"].(map[string]any)
	if !ok || len(meta) != 3 || meta["json_schema_version"] != json.Number("1") {
		return nil, false
	}
	version, versionOK := meta["version"].(string)
	_, releaseOK := meta["release_name"].(string)
	if !versionOK || version == "" || !releaseOK {
		return nil, false
	}
	objects := make([]map[string]any, 0, len(array)-1)
	for _, entry := range array[1:] {
		object, ok := entry.(map[string]any)
		if !ok {
			return nil, false
		}
		objects = append(objects, object)
	}
	return objects, true
}

// Reject duplicate keys, trailing JSON and excessive nesting. Ordinary
// encoding/json map decoding silently accepts duplicate keys, which is not an
// unambiguous firewall observation.
func exitGuardJSONValue(decoder *json.Decoder, depth int) (any, bool) {
	if depth > 32 {
		return nil, false
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, false
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return token, true
	}
	switch delimiter {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			token, err := decoder.Token()
			key, ok := token.(string)
			if err != nil || !ok {
				return nil, false
			}
			if _, exists := object[key]; exists {
				return nil, false
			}
			value, ok := exitGuardJSONValue(decoder, depth+1)
			if !ok {
				return nil, false
			}
			object[key] = value
		}
		end, err := decoder.Token()
		return object, err == nil && end == json.Delim('}')
	case '[':
		array := []any{}
		for decoder.More() {
			value, ok := exitGuardJSONValue(decoder, depth+1)
			if !ok {
				return nil, false
			}
			array = append(array, value)
		}
		end, err := decoder.Token()
		return array, err == nil && end == json.Delim(']')
	default:
		return nil, false
	}
}
