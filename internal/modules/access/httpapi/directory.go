package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
)

func directoryOptions(p access.Page) response.Options {
	var next *string
	if p.More {
		next = &p.Next
	}
	return response.Options{Meta: &response.Meta{Pagination: response.Pagination{Limit: p.Limit, NextCursor: next, HasMore: p.More}}}
}

// fitDirectoryPage measures the same HTML-escaped JSON as the shared writer.
// Reserve the larger nonterminal envelope, including the encoder's newline,
// before admitting whole rows. The SQL limit/lookahead and requested limit stay
// unchanged; a byte-limited page resumes after its last returned account ID.
func fitDirectoryPage(r *http.Request, p access.Page) (access.Page, error) {
	reserved := p
	reserved.More = true
	reserved.Next = "00000000000000000000000000000000"
	envelope, err := json.Marshal(response.SuccessEnvelope{
		Success: true, Code: "USERS_FETCHED", Data: []access.User{},
		RequestID: requestmeta.ID(r.Context()), Meta: directoryOptions(reserved).Meta,
	})
	if err != nil {
		return p, apperror.New(apperror.Internal, err)
	}
	size := len(envelope) + 1 // json.Encoder appends a newline.
	count := 0
	for _, u := range p.Users {
		row, err := json.Marshal(u)
		if err != nil {
			return p, apperror.New(apperror.Internal, err)
		}
		extra := len(row)
		if count > 0 {
			extra++
		} // array comma
		if extra > response.MaxJSONBytes-size {
			break
		}
		size += extra
		count++
	}
	if count < len(p.Users) {
		// Valid schema-bounded profiles always fit individually. Never emit an
		// empty has_more page that cannot advance if storage violates that bound.
		if count == 0 {
			return p, apperror.New(apperror.Internal, nil)
		}
		p.Users = p.Users[:count]
		p.More = true
		p.Next = p.Users[count-1].AccountID
	}
	return p, nil
}
