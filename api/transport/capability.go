package transport

import (
	"encoding/json"
	"sort"
)

type Capability struct {
	Engine          string `json:"engine"`
	ProtocolVersion string `json:"protocol_version"`
	ProfileVersion  uint32 `json:"profile_version"`
}

type Capabilities struct {
	Version uint32       `json:"version"`
	Engines []Capability `json:"engines"`
}

func ValidateCapabilities(c Capabilities) error {
	if c.Version != Version {
		return ErrUnsupported
	}
	if len(c.Engines) == 0 || len(c.Engines) > MaxProfiles {
		return ErrInvalid
	}
	seen := make(map[Capability]bool, len(c.Engines))
	for _, engine := range c.Engines {
		if !supported(engine.Engine, engine.ProtocolVersion) || engine.ProfileVersion != Version {
			return ErrUnsupported
		}
		if seen[engine] {
			return ErrInvalid
		}
		seen[engine] = true
	}
	return nil
}

func DecodeCapabilities(raw []byte) (Capabilities, error) {
	var c Capabilities
	if decode(raw, MaxProfileBytes, &c) != nil {
		return Capabilities{}, ErrInvalid
	}
	if err := ValidateCapabilities(c); err != nil {
		return Capabilities{}, err
	}
	return c, nil
}

func CanonicalCapabilities(c Capabilities) ([]byte, error) {
	if err := ValidateCapabilities(c); err != nil {
		return nil, err
	}
	c.Engines = append([]Capability(nil), c.Engines...)
	sort.Slice(c.Engines, func(i, j int) bool { return c.Engines[i].Engine < c.Engines[j].Engine })
	return json.Marshal(c)
}

// CommonProfile checks only mutually supported format/engine semantics. The
// Coordinator and Client must independently enforce grants and signed binding.
func CommonProfile(p Profile, left, right Capabilities) bool {
	if ValidateProfile(p) != nil || ValidateCapabilities(left) != nil || ValidateCapabilities(right) != nil {
		return false
	}
	wanted := Capability{p.Engine, p.ProtocolVersion, p.Version}
	found := func(c Capabilities) bool {
		for _, engine := range c.Engines {
			if engine == wanted {
				return true
			}
		}
		return false
	}
	return found(left) && found(right)
}
