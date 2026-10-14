package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a time.Duration that marshals to a Go duration string (e.g. "15s")
// in JSON and unmarshals from either such a string or a plain number of seconds.
type Duration time.Duration

// Std returns the underlying time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// MarshalJSON renders the duration as a string such as "15s".
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON accepts a duration string ("15s") or a number of seconds (15).
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch val := v.(type) {
	case string:
		parsed, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", val, err)
		}
		*d = Duration(parsed)
	case float64:
		*d = Duration(time.Duration(val) * time.Second)
	default:
		return fmt.Errorf("invalid duration value %v (want string or number)", v)
	}
	return nil
}
