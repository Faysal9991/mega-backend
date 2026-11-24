// Package validation handles transport decoding and bounded semantic diagnostics.
package validation

import (
	"cmp"
	"slices"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
)

// Result is local to one validation operation; it contains no business rules.
type Result struct {fields []apperror.Field;invalid bool}

// Add accepts only static, reviewed public metadata. It keeps the first 20
// lexical diagnostics deterministically, independent of insertion order.
func (r *Result) Add(field,code,message string) {
	entry:=apperror.Field{Field:field,Code:code,Message:message}
	if !apperror.ValidField(entry){r.invalid=true;return}
	if slices.Contains(r.fields,entry){return}
	r.fields=append(r.fields,entry)
	slices.SortFunc(r.fields,func(a,b apperror.Field)int{
		if n:=cmp.Compare(a.Field,b.Field);n!=0{return n}
		if n:=cmp.Compare(a.Code,b.Code);n!=0{return n}
		return cmp.Compare(a.Message,b.Message)
	})
	if len(r.fields)>apperror.MaxFields {r.fields=r.fields[:apperror.MaxFields]}
}

func (r *Result) Err() error {
	if r.invalid{return apperror.New(apperror.Internal,nil)}
	if len(r.fields)==0{return nil}
	return apperror.Validation(r.fields)
}
