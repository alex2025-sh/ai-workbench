package schema

import (
	"encoding/json"
	"testing"
)

func TestMask(t *testing.T) {
	s := json.RawMessage(`{"type":"object","properties":{
	  "uid":{"type":"string","x-pii":"mask_middle"},
	  "secret":{"type":"string","x-pii":"drop"},
	  "card":{"type":"string","x-pii":"hash"},
	  "name":{"type":"string"}}}`)
	in := map[string]any{"uid": "10000001", "secret": "x", "card": "4111", "name": "VIP"}
	out := Mask(s, in)
	if out["uid"] != "10****01" || out["name"] != "VIP" || len(out["card"].(string)) != 12 {
		t.Fatalf("got %v", out)
	}
	if _, ok := out["secret"]; ok {
		t.Fatal("drop field must be removed")
	}
	if in["uid"] != "10000001" {
		t.Fatal("input must not be modified")
	}
}
