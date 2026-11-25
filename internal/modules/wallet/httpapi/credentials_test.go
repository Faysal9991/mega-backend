package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFinancialCredentialsRejectAmbiguousHeaders(t *testing.T) {
	token, installation := strings.Repeat("a", 43), strings.Repeat("b", 32)
	r := httptest.NewRequest("GET", "/api/v1/wallet", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Installation-ID", installation)
	if c := credentials(r); c.Access != token || c.Installation != installation {
		t.Fatal("valid bound bearer lost")
	}
	r.Header.Add("Authorization", "Bearer "+strings.Repeat("c", 43))
	if c := credentials(r); c.Access != "" || c.Installation != "" {
		t.Fatal("ambiguous bearer accepted")
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Add("X-Installation-ID", strings.Repeat("d", 32))
	if c := credentials(r); c.Access != "" || c.Installation != "" {
		t.Fatal("ambiguous installation accepted")
	}
	r.Header.Set("X-Installation-ID", installation)
	r.Header.Set("Authorization", "Bearer "+token+" ")
	if c := credentials(r); c.Access != "" {
		t.Fatal("bearer whitespace accepted")
	}
}
