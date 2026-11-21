package a2a

import "encoding/json"

// MarshalJSON implements json.Marshaler. The REQUIRED lists encode as [] when
// unset rather than null, which a strict 1.0 client rejects.
func (card AgentCard) MarshalJSON() ([]byte, error) {
	type plain AgentCard
	out := plain(card)
	if out.Skills == nil {
		out.Skills = []AgentSkill{}
	}
	if out.SupportedInterfaces == nil {
		out.SupportedInterfaces = []AgentInterface{}
	}
	out.DefaultInputModes = nonNil(out.DefaultInputModes)
	out.DefaultOutputModes = nonNil(out.DefaultOutputModes)
	return json.Marshal(out)
}

// MarshalJSON implements json.Marshaler. Tags is REQUIRED and encodes as []
// when unset.
func (s AgentSkill) MarshalJSON() ([]byte, error) {
	type plain AgentSkill
	out := plain(s)
	out.Tags = nonNil(out.Tags)
	return json.Marshal(out)
}

// UnmarshalJSON implements json.Unmarshaler. A 0.3 skill declares its
// requirements in "security"; they are folded into SecurityRequirements when
// the skill does not carry the 1.0 field itself.
func (s *AgentSkill) UnmarshalJSON(data []byte) error {
	type plain AgentSkill
	var wire struct {
		plain
		Security []SecurityRequirement `json:"security"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*s = AgentSkill(wire.plain)
	if len(s.SecurityRequirements) == 0 {
		s.SecurityRequirements = wire.Security
	}
	return nil
}
