package events

import (
	"encoding/json"
	"hash/fnv"
	"sort"
	"strconv"
)

// Event is one ingested record after parsing, filtering, mapping
// and rendering. TS is unix milliseconds. Payload is an arbitrary
// JSON object; Render is the human-readable string produced by the
// source's text/template.
type Event struct {
	ID      string
	TS      int64
	Source  string
	Kind    string
	Payload map[string]any
	Render  string
}

// FNV1aID computes the stable row id used for INSERT OR IGNORE
// dedupe: fnv1a(source || "\x00" || ts || "\x00" || kind || "\x00" || canonical(payload)).
// Payload is canonicalised by sorting keys so two equal maps
// produce equal ids regardless of map iteration order.
func FNV1aID(source string, ts int64, kind string, payload map[string]any) string {
	h := fnv.New64a()
	h.Write([]byte(source))
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatInt(ts, 10)))
	h.Write([]byte{0})
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(canonicalJSON(payload)))
	return strconv.FormatUint(h.Sum64(), 16)
}

// canonicalJSON returns a JSON encoding with keys sorted at every
// object level so logically equal payloads hash to the same id.
func canonicalJSON(v any) string {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b []byte
		b = append(b, '{')
		for i, k := range keys {
			if i > 0 {
				b = append(b, ',')
			}
			kb, _ := json.Marshal(k)
			b = append(b, kb...)
			b = append(b, ':')
			b = append(b, canonicalJSON(x[k])...)
		}
		b = append(b, '}')
		return string(b)
	default:
		out, _ := json.Marshal(v)
		return string(out)
	}
}
