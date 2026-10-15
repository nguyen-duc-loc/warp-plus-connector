package terminal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"regexp"
	"unicode/utf8"
)

const requestLimit = 4096
const replyLimit = 1024 * 1024

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

type request struct {
	Version int             `json:"version"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// strictJSON rejects duplicate fields, case aliases, unknown fields, and trailing data.
func strictJSON(raw []byte, out any) error { return strictJSONNullable(raw, out, false) }

func strictJSONNullable(raw []byte, out any, focusNullable bool) error {
	if !utf8.Valid(raw) {
		return failure("ui_protocol_invalid")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var check func() error
	check = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return err
					}
					s, ok := key.(string)
					if !ok || seen[s] {
						return failure("ui_protocol_invalid")
					}
					seen[s] = true
					if err := check(); err != nil {
						return err
					}
				}
			case '[':
				for d.More() {
					if err := check(); err != nil {
						return err
					}
				}
			default:
				return failure("ui_protocol_invalid")
			}
			_, err = d.Token()
		}
		return err
	}
	if err := check(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return failure("ui_protocol_invalid")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	// Matching the exact key set also rejects missing fields and JSON's case insensitive aliases.
	var actual, expected map[string]json.RawMessage
	if json.Unmarshal(raw, &actual) == nil && actual != nil {
		encoded, err := json.Marshal(out)
		if err != nil || json.Unmarshal(encoded, &expected) != nil || len(actual) != len(expected) {
			return failure("ui_protocol_invalid")
		}
		for key, value := range actual {
			// Input and manifest fields are required and nonnullable. Go otherwise
			// silently decodes null into a scalar's zero value, including an empty commit.
			nullableInput := focusNullable && (key == "focus_id" || key == "candidate_id" ||
				key == "page_limit" || key == "source_id" || key == "page_number" || key == "attempt_id")
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !nullableInput {
				return failure("ui_protocol_invalid")
			}
			if _, ok := expected[key]; !ok {
				return failure("ui_protocol_invalid")
			}
		}
	}
	return nil
}

func readRequest(r *bufio.Reader, previous *big.Int) (request, error) {
	var out request
	raw, err := r.ReadSlice('\n')
	if err == io.EOF && len(raw) == 0 {
		return out, io.EOF
	}
	if err != nil || len(raw) > requestLimit || strictJSON(raw, &out) != nil || out.Version != 3 ||
		!decimalPattern.MatchString(out.ID) || len(out.ID) > 128 {
		return out, failure("ui_protocol_invalid")
	}
	id, _ := new(big.Int).SetString(out.ID, 10)
	if id.Cmp(previous) <= 0 {
		return out, failure("ui_protocol_invalid")
	}
	previous.Set(id)
	return out, nil
}

func writeReply(w io.Writer, id string, result any) error {
	raw, err := json.Marshal(map[string]any{"version": 3, "id": id, "result": result})
	if err != nil || len(raw)+1 > replyLimit {
		return failure("ui_protocol_invalid")
	}
	raw = append(raw, '\n')
	n, err := w.Write(raw)
	if err != nil || n != len(raw) {
		return failure("ui_protocol_invalid")
	}
	return nil
}
