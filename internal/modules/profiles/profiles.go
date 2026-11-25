// Package profiles owns editable profile data independently of identity.
package profiles

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"unicode"
	"unicode/utf8"
)

type Profile struct {
	AccountID   string `json:"account_id"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	AvatarURL   string `json:"avatar_url"`
}

// Pointer presence distinguishes omitted fields from intentional empty strings.
// HTTP additionally rejects explicit null before constructing this value.
type Patch struct{ DisplayName, Bio *string }

func (p Patch) Validate() error {
	if p.DisplayName == nil && p.Bio == nil {
		return apperror.New(apperror.ValidationFailed, nil)
	}
	for _, v := range []struct {
		value        *string
		bytes, runes int
		multiline    bool
	}{{p.DisplayName, 256, 80, false}, {p.Bio, 2048, 500, true}} {
		if v.value == nil {
			continue
		}
		s := *v.value
		if !utf8.ValidString(s) || len(s) > v.bytes || utf8.RuneCountInString(s) > v.runes {
			return apperror.New(apperror.ValidationFailed, nil)
		}
		for _, r := range s {
			if unicode.IsControl(r) && !(v.multiline && (r == '\n' || r == '\t')) {
				return apperror.New(apperror.ValidationFailed, nil)
			}
		}
	}
	return nil
}
func (p Patch) Fields() []string {
	out := []string{}
	if p.DisplayName != nil {
		out = append(out, "display_name")
	}
	if p.Bio != nil {
		out = append(out, "bio")
	}
	return out
}

// Initializer is bound to the caller's transaction by the composition adapter.
// Only signup invokes it; returning login never updates profile data.
type Initializer interface {
	Initialize(context.Context, string, string, string) error
}
