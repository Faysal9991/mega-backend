package access

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/modules/profiles"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
)

// The Python OpenAPI validator executes the same values against the published
// schema. These assertions preserve Go's exact policy, especially terminal LF.
func TestStep9SharedValidationBoundaries(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/openapi/fixtures/step9_validation.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Field, Name, Value string
		Valid              bool
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Field+"/"+f.Name, func(t *testing.T) {
			var valid bool
			switch f.Field {
			case "display_name":
				valid = (profiles.Patch{DisplayName: &f.Value}).Validate() == nil
			case "bio":
				valid = (profiles.Patch{Bio: &f.Value}).Validate() == nil
			case "role_name":
				valid = validText(f.Value, 64)
			case "reason":
				valid = ValidReason(f.Value)
			case "account_id", "role_id", "cursor":
				valid = ValidID(f.Value)
			case "installation_id":
				valid = identity.ValidInstallation(f.Value)
			case "request_id":
				valid = requestmeta.ValidID(f.Value)
			default:
				t.Fatal("unknown fixture field")
			}
			if valid != f.Valid {
				t.Fatalf("runtime validation = %t, expected %t", valid, f.Valid)
			}
		})
	}
}
