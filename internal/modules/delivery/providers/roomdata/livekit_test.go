package roomdata

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/proto"
)

func TestServerOnlyBoundedGiftData(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		b, e := io.ReadAll(io.LimitReader(r.Body, 8192))
		if e != nil {
			t.Error(e)
		}
		var request livekit.SendDataRequest
		if e = proto.Unmarshal(b, &request); e != nil {
			t.Error(e)
		}
		if request.Room != "immutable-session" || request.Kind != livekit.DataPacket_RELIABLE || request.GetTopic() != "room.gift.v1" || string(request.Data) != `{"event_id":"synthetic"}` {
			t.Error("unsafe data projection")
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("server authorization missing")
		}
		w.Header().Set("Content-Type", "application/protobuf")
		out, _ := proto.Marshal(&livekit.SendDataResponse{})
		w.Write(out)
	}))
	defer server.Close()
	p := NewLiveKit(server.URL, "synthetic-key", "synthetic-server-secret-for-unit-test")
	if e := p.SendGift(context.Background(), "immutable-session", []byte(`{"event_id":"synthetic"}`)); e != nil || !called {
		t.Fatal("SDK data send", e)
	}
	called = false
	if e := p.SendGift(context.Background(), "s", make([]byte, 4097)); e == nil || called {
		t.Fatal("payload bound")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := p.SendGift(ctx, "s", []byte("{}")); e == nil {
		t.Fatal("cancellation ignored")
	}
}
