package srvrepo

import (
	"encoding/json"
	"time"
)

// timeFormatJSON defines the format that we use for time formatting in JSON.
const timeFormatJSON = time.RFC3339

// jsonTime defines a time.Time with custom marshalling (embedded for method
// access, rather than aliasing)
type jsonTime struct {
	time.Time
	// put nothing else here...
}

// MarshalJSON satisfies the encoding/json.Marshaler interface and customizes
// the JSON formatting of the jsonTime structure.
func (t jsonTime) MarshalJSON() ([]byte, error) {
	formatted := t.Format(timeFormatJSON)

	return json.Marshal(&formatted)
}
