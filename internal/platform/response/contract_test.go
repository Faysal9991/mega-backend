package response

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
)

// Fixtures are compared to real writer output; external OpenAPI validation checks
// these same fixtures against common schemas without registering runtime routes.
func TestOpenAPIContractFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/openapi/fixtures/envelopes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Schema string
		Status       int
		Body         map[string]json.RawMessage
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			wr, r, rec, w, _ := setup(t)
			if fixture.Schema == "ErrorEnvelope" {
				var code apperror.Code
				if err := json.Unmarshal(fixture.Body["code"], &code); err != nil {
					t.Fatal(err)
				}
				appErr := apperror.New(code, nil)
				if code == apperror.ValidationFailed {
					appErr = apperror.Validation([]apperror.Field{{Field: "amount", Code: "INVALID_AMOUNT", Message: "Must be positive"}})
				}
				if err := wr.Error(w, r, appErr); err != nil {
					t.Fatal(err)
				}
				if rec.Code != fixture.Status {
					t.Fatal("status drift")
				}
			} else {
				var code string
				var data any
				var options Options
				if err := json.Unmarshal(fixture.Body["code"], &code); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(fixture.Body["data"], &data); err != nil {
					t.Fatal(err)
				}
				if meta, ok := fixture.Body["meta"]; ok {
					if err := json.Unmarshal(meta, &options.Meta); err != nil {
						t.Fatal(err)
					}
				}
				status := 200
				if fixture.Name == "accepted" {
					status = 202
				}
				if err := wr.Success(w, r, status, code, data, options); err != nil {
					t.Fatal(err)
				}
			}
			var actual, want any
			if err := json.Unmarshal(rec.Body.Bytes(), &actual); err != nil {
				t.Fatal(err)
			}
			fixtureJSON, err := json.Marshal(fixture.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(fixtureJSON, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("contract fixture drift: got %s; want %s", rec.Body, fixtureJSON)
			}
		})
	}
	raw, err = os.ReadFile("../../../contracts/openapi/common.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths      map[string]any
		Components struct {
			Schemas map[string]struct{ Enum []apperror.Code }
		}
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Paths) != 0 || !reflect.DeepEqual(spec.Components.Schemas["ErrorCode"].Enum, apperror.Codes()) {
		t.Fatal("OpenAPI scope/registry drift")
	}
}
