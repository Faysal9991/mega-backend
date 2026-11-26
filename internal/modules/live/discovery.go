package live

import (
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"strconv"
	"strings"
)

type RankedRoom struct {
	Room
	Priority int32 `json:"live_listing_priority"`
}
type ListCursor struct {
	Snapshot, Room string
	Priority       int32
}

func ParseListCursor(s string) (ListCursor, bool) {
	p := strings.Split(s, ".")
	if len(p) != 3 || !access.ValidID(p[0]) || !access.ValidID(p[2]) {
		return ListCursor{}, false
	}
	n, e := strconv.ParseInt(p[1], 10, 32)
	if e != nil || n < 0 || n > 1000000 || strconv.FormatInt(n, 10) != p[1] {
		return ListCursor{}, false
	}
	return ListCursor{p[0], p[2], int32(n)}, true
}
func (c ListCursor) String() string {
	return c.Snapshot + "." + strconv.FormatInt(int64(c.Priority), 10) + "." + c.Room
}
