// Package delivery defines catalog, asset and notification delivery contracts.
// Financial posting belongs to Wallet; provider delivery is never settlement.
package delivery

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
)

const (
	GiftsManage   = "gifts.manage"
	MediaManage   = "media.manage"
	Announce      = "notifications.announce"
	Inspect       = "deliveries.read"
	MaxImageBytes = int64(2 << 20)
)

func Fail(c apperror.Code) error { return apperror.New(c, nil) }
func Text(s string, max int) bool {
	if !utf8.ValidString(s) || len(s) > max {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func ValidCode(s string) bool { return codePattern.MatchString(s) }

type Gift struct {
	ID               string    `json:"gift_id"`
	Code             string    `json:"code"`
	VersionID        string    `json:"version_id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Price            int64     `json:"diamond_price"`
	Category         string    `json:"category"`
	ThumbnailAssetID string    `json:"thumbnail_asset_id"`
	ThumbnailURL     string    `json:"thumbnail_url"`
	AnimationURL     string    `json:"animation_url"`
	Active           bool      `json:"active"`
	DisplayOrder     int32     `json:"display_order"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (g Gift) Validate() error {
	if !ValidCode(g.Code) || g.Name == "" || !Text(g.Name, 128) || !Text(g.Description, 1024) || g.Price < 1 || g.Category == "" || !Text(g.Category, 64) || g.DisplayOrder < 0 || g.DisplayOrder > 1000000 || g.ThumbnailAssetID != "" && !access.ValidID(g.ThumbnailAssetID) {
		return Fail(apperror.ValidationFailed)
	}
	return nil
}

type Asset struct {
	ID           string    `json:"asset_id"`
	Purpose      string    `json:"purpose"`
	State        string    `json:"state"`
	URL          string    `json:"url"`
	ThumbnailURL string    `json:"thumbnail_url"`
	Width        int32     `json:"width"`
	Height       int32     `json:"height"`
	Size         int64     `json:"size_bytes"`
	ErrorCode    string    `json:"error_code"`
	ExpiresAt    time.Time `json:"expires_at"`
}
type Upload struct {
	Purpose     string `json:"purpose"`
	Target      string `json:"target_id"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
}

func (u Upload) Validate() error {
	sum, e := base64.StdEncoding.DecodeString(u.SHA256)
	if e != nil || len(sum) != 32 || u.Size < 1 || u.Size > MaxImageBytes || u.ContentType != "image/png" && u.ContentType != "image/jpeg" || u.Purpose != "avatar" && u.Purpose != "agency_logo" && u.Purpose != "gift_thumbnail" {
		return Fail(apperror.ValidationFailed)
	}
	return nil
}

type SignedUpload struct {
	Asset     Asset       `json:"asset"`
	URL       string      `json:"upload_url"`
	Headers   http.Header `json:"required_headers"`
	ExpiresAt time.Time   `json:"upload_expires_at"`
}
type Object struct {
	Size              int64
	ContentType, ETag string
}
type ObjectStore interface {
	Presign(context.Context, string, string, int64, string, time.Duration) (string, http.Header, error)
	Head(context.Context, string) (Object, error)
	Read(context.Context, string, string, int64) ([]byte, error)
	Put(context.Context, string, []byte, string) error
	Delete(context.Context, string) error
}
type UnavailableStore struct{}

func (UnavailableStore) Presign(context.Context, string, string, int64, string, time.Duration) (string, http.Header, error) {
	return "", nil, errors.New("storage unavailable")
}
func (UnavailableStore) Head(context.Context, string) (Object, error) {
	return Object{}, errors.New("storage unavailable")
}
func (UnavailableStore) Read(context.Context, string, string, int64) ([]byte, error) {
	return nil, errors.New("storage unavailable")
}
func (UnavailableStore) Put(context.Context, string, []byte, string) error {
	return errors.New("storage unavailable")
}
func (UnavailableStore) Delete(context.Context, string) error {
	return errors.New("storage unavailable")
}

type Preferences struct {
	GiftReceived      bool `json:"gift_received"`
	AgencyDecision    bool `json:"agency_decision"`
	AccountEvent      bool `json:"account_event"`
	RoomInvitation    bool `json:"room_invitation"`
	AdminAnnouncement bool `json:"admin_announcement"`
}
type Push struct{ Token, Kind, EventID, Template string }
type PushProvider interface {
	Send(context.Context, Push) error
}
type RoomProvider interface {
	SendGift(context.Context, string, []byte) error
}

var (
	ErrInvalidToken = errors.New("invalid registration")
	ErrPermanent    = errors.New("permanent provider rejection")
)

func Template(kind string) (string, string, bool) {
	switch kind {
	case "gift_received":
		return "Gift received", "You received a gift.", true
	case "agency_decision":
		return "Agency application", "Your application status changed.", true
	case "account_event":
		return "Account update", "Review your account activity.", true
	case "room_invitation":
		return "Room invitation", "You have a room invitation.", true
	case "admin_announcement":
		return "Announcement", "A new announcement is available.", true
	}
	return "", "", false
}
func Seal(key []byte, token, installation string) ([]byte, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return a.Seal(nonce, nonce, []byte(token), []byte(installation)), nil
}
func Open(key, data []byte, installation string) (string, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return "", e
	}
	a, e := cipher.NewGCM(block)
	if e != nil || len(data) < a.NonceSize() {
		return "", errors.New("invalid encrypted registration")
	}
	plain, e := a.Open(nil, data[:a.NonceSize()], data[a.NonceSize():], []byte(installation))
	return string(plain), e
}
func Hash(s string) string              { x := sha256.Sum256([]byte(s)); return hex.EncodeToString(x[:]) }
func Fingerprint(v any) (string, error) { b, e := json.Marshal(v); return Hash(string(b)), e }
func AnimationAllowed(raw string, hosts []string) bool {
	if raw == "" {
		return true
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(raw) > 512 || u.Port() != "" {
		return false
	}
	for _, h := range hosts {
		if strings.EqualFold(u.Hostname(), h) && strings.HasPrefix(u.Path, "/") && !strings.Contains(u.Path, "..") {
			return strings.HasSuffix(u.Path, ".json") || strings.HasSuffix(u.Path, ".webm") || strings.HasSuffix(u.Path, ".gif")
		}
	}
	return false
}

func PushTemplate(kind, code string) (string, string, bool) {
	if kind == "admin_announcement" {
		switch code {
		case "maintenance":
			return "Maintenance", "Scheduled maintenance may affect availability.", true
		case "service_update":
			return "Service update", "A service update is available.", true
		case "community_update":
			return "Community update", "A community update is available.", true
		default:
			return "", "", false
		}
	}
	return Template(kind)
}

type Page struct {
	Data  any
	Next  string
	More  bool
	Limit int
}

func ValidPage(cursor string, limit int) bool {
	return limit > 0 && limit <= 50 && (cursor == "" || access.ValidID(cursor))
}

type GiftChange struct {
	Gift       Gift   `json:"gift"`
	PreviousID string `json:"previous_version_id"`
	Reason     string `json:"reason"`
}
type Announcement struct {
	Accounts []string `json:"account_ids"`
	Template string   `json:"template"`
	Reason   string   `json:"reason"`
}
