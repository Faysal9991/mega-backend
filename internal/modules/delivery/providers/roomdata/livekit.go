package roomdata

import (
	"context"
	"errors"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

type LiveKit struct{ client *lksdk.RoomServiceClient }

func NewLiveKit(url, key, secret string) *LiveKit {
	return &LiveKit{lksdk.NewRoomServiceClient(url, key, secret)}
}
func (l *LiveKit) SendGift(ctx context.Context, session string, data []byte) error {
	if len(data) > 4096 {
		return errors.New("event exceeds bound")
	}
	_, e := l.client.SendData(ctx, &livekit.SendDataRequest{Room: session, Data: data, Kind: livekit.DataPacket_RELIABLE, Topic: func() *string { v := "room.gift.v1"; return &v }()})
	return e
}
