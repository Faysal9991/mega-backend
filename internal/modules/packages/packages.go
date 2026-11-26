// Package packages owns configurable commercial entitlements, never wallet balances.
package packages

import (
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"regexp"
	"slices"
	"time"
)

type Definition struct {
	ID          string    `json:"package_id"`
	Code        string    `json:"code"`
	Version     string    `json:"version_id"`
	Previous    string    `json:"previous_version_id,omitempty"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       int64     `json:"diamond_price"`
	Duration    int64     `json:"duration_seconds"`
	Powers      []string  `json:"powers"`
	Badge       string    `json:"badge_asset_id"`
	Entrance    string    `json:"entrance_asset_id"`
	Priority    int32     `json:"live_listing_priority"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
}

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func (d Definition) Validate() error {
	if !codePattern.MatchString(d.Code) || !live.Text(d.Name, 128) || d.Description != "" && !live.Text(d.Description, 1024) || d.Price <= 0 || d.Duration < 60 || d.Duration > 31536000 || d.Priority < 0 || d.Priority > 1000000 || d.Powers == nil || len(d.Powers) > 5 || d.Previous != "" && !access.ValidID(d.Previous) || d.Badge != "" && !access.ValidID(d.Badge) || d.Entrance != "" && !access.ValidID(d.Entrance) {
		return access.Failure(apperror.ValidationFailed)
	}
	seen := []string{}
	for _, p := range d.Powers {
		if !access.PackagePower(p) || slices.Contains(seen, p) {
			return access.Failure(apperror.Forbidden)
		}
		seen = append(seen, p)
	}
	return nil
}

type Entitlement struct {
	ID        string    `json:"entitlement_id"`
	AccountID string    `json:"account_id"`
	PackageID string    `json:"package_id"`
	StartsAt  time.Time `json:"starts_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Source    string    `json:"source"`
	Powers    []string  `json:"powers"`
}
type Purchase struct {
	AccountID       string    `json:"account_id"`
	CreatedAt       time.Time `json:"created_at"`
	StartsAt        time.Time `json:"starts_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	OperationID     string    `json:"operation_id"`
	PackageID       string    `json:"package_id"`
	VersionID       string    `json:"version_id"`
	EntitlementID   string    `json:"entitlement_id"`
	Price           int64     `json:"diamond_price"`
	FinancialStatus string    `json:"financial_status"`
}
type Command struct {
	Kind       string
	ID         string
	Target     string
	Key        string
	Reason     string
	Definition *Definition
}
type Query struct {
	Kind, ID, Cursor string
	Limit            int
}
type Page struct {
	Items any
	Next  string
	More  bool
	Limit int
}
