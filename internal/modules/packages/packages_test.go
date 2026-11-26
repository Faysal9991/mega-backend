package packages

import (
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"testing"
)

func TestPurchasablePowerBoundary(t *testing.T) {
	d := Definition{Code: "celebrity", Name: "Celebrity", Price: 100, Duration: 3600, Powers: []string{"room.seat.self_join", "benefit.badge"}, Active: true}
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, p := range access.Capabilities() {
		d.Powers = []string{p.Action}
		e := d.Validate()
		if (e == nil) != p.PackageSafe {
			t.Fatalf("power boundary %s", p.Action)
		}
	}
	d.Powers = []string{}
	d.Price = 0
	if d.Validate() == nil {
		t.Fatal("free invalid configuration")
	}
	d.Price = 1
	d.Duration = 1
	if d.Validate() == nil {
		t.Fatal("invalid duration")
	}
}
