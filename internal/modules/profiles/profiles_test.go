package profiles

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestUnicodePartialUpdatePolicy(t *testing.T) {
	cases := []struct {
		name, bio *string
		valid     bool
	}{{nil, nil, false}, {ptr(""), nil, true}, {nil, ptr(""), true}, {ptr(strings.Repeat("ব", 80)), nil, true}, {ptr(strings.Repeat("ব", 81)), nil, false}, {ptr(strings.Repeat("😀", 64)), nil, true}, {ptr(strings.Repeat("😀", 65)), nil, false}, {nil, ptr(strings.Repeat("ব", 500)), true}, {nil, ptr(strings.Repeat("a", 501)), false}, {ptr("line\nname"), nil, false}, {nil, ptr("line\n\ttwo"), true}, {ptr(string([]byte{0xff})), nil, false}, {nil, ptr("\x00"), false}}
	for i, c := range cases {
		if e := (Patch{c.name, c.bio}).Validate(); (e == nil) != c.valid {
			t.Fatalf("case %d policy drift", i)
		}
	}
}
func ptr(s string) *string { return &s }

func TestSharedUnicodeContractFixtures(t *testing.T) {
	raw, e := os.ReadFile("../../../contracts/openapi/fixtures/profile_utf8.json")
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Field, Value string
		Valid        bool
	}
	if e := json.Unmarshal(raw, &cases); e != nil {
		t.Fatal(e)
	}
	for i, c := range cases {
		p := Patch{}
		if c.Field == "display_name" {
			p.DisplayName = &c.Value
		} else {
			p.Bio = &c.Value
		}
		if (p.Validate() == nil) != c.Valid {
			t.Fatalf("fixture %d drift", i)
		}
	}
}

func TestSharedPointerDoesNotAllowNameControls(t *testing.T) {
	value := "line\nname"
	if (Patch{DisplayName: &value, Bio: &value}).Validate() == nil {
		t.Fatal("bio control policy applied to display name")
	}
}
