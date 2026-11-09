package events

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Event data types with an `error` field serialize it as its message string.
// encoding/json writes an error value as "{}" and cannot decode anything back
// into the error interface, so without these methods an event with an error
// set lost its whole payload on a store round trip: FileEventStore.Query,
// Stream and recordings returned it with nil Data. A nil error is written as
// null, as before; a decoded error carries the original message.

// errorFieldKey is the JSON key of the Error field on every type below.
const errorFieldKey = "Error"

// legacyErrorMessage is the message of an error decoded from data written
// before these methods existed, which recorded every error as {}: the event
// failed, but its message was never stored.
const legacyErrorMessage = "error (message not recorded)"

// marshalErrorField marshals plain, whose Error field the caller has cleared,
// with Error written as err's message (null for a nil err).
func marshalErrorField[P any](plain P, err error) ([]byte, error) {
	b, mErr := json.Marshal(plain)
	if mErr != nil {
		return nil, mErr
	}
	var fields map[string]json.RawMessage
	if uErr := json.Unmarshal(b, &fields); uErr != nil {
		return nil, uErr
	}
	var msg *string
	if err != nil {
		s := err.Error()
		msg = &s
	}
	if fields[errorFieldKey], mErr = json.Marshal(msg); mErr != nil {
		return nil, mErr
	}
	return json.Marshal(fields)
}

// unmarshalErrorField decodes b into plain, except for the Error key, whose
// message it returns (nil when the key is absent or null).
func unmarshalErrorField[P any](b []byte, plain *P) (*string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	var msg *string
	if raw, ok := fields[errorFieldKey]; ok {
		if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '{' {
			// Written before errors were serialized as messages: "Error":{}.
			legacy := legacyErrorMessage
			msg = &legacy
		} else if err := json.Unmarshal(raw, &msg); err != nil {
			return nil, err
		}
		delete(fields, errorFieldKey)
	}
	rest, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return msg, json.Unmarshal(rest, plain)
}

// errorFromMessage rebuilds a serialized error; a nil message is a nil error.
func errorFromMessage(msg *string) error {
	if msg == nil {
		return nil
	}
	return errors.New(*msg)
}

// MarshalJSON writes Error as its message.
func (d PipelineFailedData) MarshalJSON() ([]byte, error) {
	type plain PipelineFailedData
	p := plain(d)
	p.Error = nil
	return marshalErrorField(p, d.Error)
}

// UnmarshalJSON reads Error back from its message.
func (d *PipelineFailedData) UnmarshalJSON(b []byte) error {
	type plain PipelineFailedData
	msg, err := unmarshalErrorField(b, (*plain)(d))
	if err != nil {
		return err
	}
	d.Error = errorFromMessage(msg)
	return nil
}

// MarshalJSON writes Error as its message.
func (d MiddlewareEventData) MarshalJSON() ([]byte, error) {
	type plain MiddlewareEventData
	p := plain(d)
	p.Error = nil
	return marshalErrorField(p, d.Error)
}

// UnmarshalJSON reads Error back from its message.
func (d *MiddlewareEventData) UnmarshalJSON(b []byte) error {
	type plain MiddlewareEventData
	msg, err := unmarshalErrorField(b, (*plain)(d))
	if err != nil {
		return err
	}
	d.Error = errorFromMessage(msg)
	return nil
}

// MarshalJSON writes Error as its message.
func (d StageEventData) MarshalJSON() ([]byte, error) {
	type plain StageEventData
	p := plain(d)
	p.Error = nil
	return marshalErrorField(p, d.Error)
}

// UnmarshalJSON reads Error back from its message.
func (d *StageEventData) UnmarshalJSON(b []byte) error {
	type plain StageEventData
	msg, err := unmarshalErrorField(b, (*plain)(d))
	if err != nil {
		return err
	}
	d.Error = errorFromMessage(msg)
	return nil
}

// MarshalJSON writes Error as its message.
func (d ProviderCallFailedData) MarshalJSON() ([]byte, error) {
	type plain ProviderCallFailedData
	p := plain(d)
	p.Error = nil
	return marshalErrorField(p, d.Error)
}

// UnmarshalJSON reads Error back from its message.
func (d *ProviderCallFailedData) UnmarshalJSON(b []byte) error {
	type plain ProviderCallFailedData
	msg, err := unmarshalErrorField(b, (*plain)(d))
	if err != nil {
		return err
	}
	d.Error = errorFromMessage(msg)
	return nil
}

// MarshalJSON writes Error as its message.
func (d ToolCallEventData) MarshalJSON() ([]byte, error) {
	type plain ToolCallEventData
	p := plain(d)
	p.Error = nil
	return marshalErrorField(p, d.Error)
}

// UnmarshalJSON reads Error back from its message.
func (d *ToolCallEventData) UnmarshalJSON(b []byte) error {
	type plain ToolCallEventData
	msg, err := unmarshalErrorField(b, (*plain)(d))
	if err != nil {
		return err
	}
	d.Error = errorFromMessage(msg)
	return nil
}

// MarshalJSON writes Error as its message.
func (d ValidationEventData) MarshalJSON() ([]byte, error) {
	type plain ValidationEventData
	p := plain(d)
	p.Error = nil
	return marshalErrorField(p, d.Error)
}

// UnmarshalJSON reads Error back from its message.
func (d *ValidationEventData) UnmarshalJSON(b []byte) error {
	type plain ValidationEventData
	msg, err := unmarshalErrorField(b, (*plain)(d))
	if err != nil {
		return err
	}
	d.Error = errorFromMessage(msg)
	return nil
}

// MarshalJSON writes Error as its message.
func (d MediaStorageEventData) MarshalJSON() ([]byte, error) {
	type plain MediaStorageEventData
	p := plain(d)
	p.Error = nil
	return marshalErrorField(p, d.Error)
}

// UnmarshalJSON reads Error back from its message.
func (d *MediaStorageEventData) UnmarshalJSON(b []byte) error {
	type plain MediaStorageEventData
	msg, err := unmarshalErrorField(b, (*plain)(d))
	if err != nil {
		return err
	}
	d.Error = errorFromMessage(msg)
	return nil
}
