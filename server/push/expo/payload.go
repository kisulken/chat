package expo

import (
	"errors"
	expo "github.com/oliveroneill/exponent-server-sdk-golang/sdk"
	"log"
	"strconv"
	"time"

	"github.com/tinode/chat/server/drafty"
	"github.com/tinode/chat/server/push"
	"github.com/tinode/chat/server/store"
	t "github.com/tinode/chat/server/store/types"
)

// MessageData adds user ID and device token to push message. This is needed for error handling.
type MessageData struct {
	Uid t.Uid
	// FCM device token.
	DeviceId string
	Message  expo.PushMessage
}

func payloadToData(pl *push.Payload) (map[string]string, error) {
	if pl == nil {
		return nil, errors.New("empty push payload")
	}
	data := make(map[string]string)
	var err error
	data["what"] = pl.What
	if pl.Silent {
		data["silent"] = "true"
	}
	data["topic"] = pl.Topic
	data["ts"] = pl.Timestamp.Format(time.RFC3339Nano)
	// Must use "xfrom" because "from" is a reserved word. Google did not bother to document it anywhere.
	data["xfrom"] = pl.From
	if pl.What == push.ActMsg {
		data["seq"] = strconv.Itoa(pl.SeqId)
		data["mime"] = pl.ContentType
		data["content"], err = drafty.ToPlainText(pl.Content)
		if err != nil {
			return nil, err
		}

		// Trim long strings to 80 runes.
		// Check byte length first and don't waste time converting short strings.
		if len(data["content"]) > maxMessageLength {
			runes := []rune(data["content"])
			if len(runes) > maxMessageLength {
				data["content"] = string(runes[:maxMessageLength]) + "…"
			}
		}
	} else if pl.What == push.ActSub {
		data["modeWant"] = pl.ModeWant.String()
		data["modeGiven"] = pl.ModeGiven.String()
	} else {
		return nil, errors.New("unknown push type")
	}
	return data, nil
}

func clonePayload(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for key, val := range src {
		dst[key] = val
	}
	return dst
}

// PrepareNotifications creates notification payloads ready to be posted
// to push notification server for the provided receipt.
func PrepareNotifications(rcpt *push.Receipt) []MessageData {
	data, err := payloadToData(&rcpt.Payload)
	if err != nil {
		log.Println("expo push: could not parse payload;", err)
		return nil
	}

	// Device IDs to send pushes to.
	var devices map[t.Uid][]t.DeviceDef
	// Count of device IDs to push to.
	var count int
	// Devices which were online in the topic when the message was sent.
	skipDevices := make(map[string]struct{})
	if len(rcpt.To) > 0 {
		// List of UIDs for querying the database

		uids := make([]t.Uid, len(rcpt.To))
		i := 0
		for uid, to := range rcpt.To {
			uids[i] = uid
			i++
			// Some devices were online and received the message. Skip them.
			for _, deviceID := range to.Devices {
				skipDevices[deviceID] = struct{}{}
			}
		}
		devices, count, err = store.Devices.GetAll(uids...)
		if err != nil {
			log.Println("expo push: db error", err)
			return nil
		}
	}
	if count == 0 && rcpt.Channel == "" {
		return nil
	}

	var messages []MessageData
	for uid, devList := range devices {
		userData := data
		if rcpt.To[uid].Delivered > 0 {
			// Silence the push for user who have received the data interactively.
			userData = clonePayload(data)
			userData["silent"] = "true"
		}
		for i := range devList {
			d := &devList[i]
			if _, ok := skipDevices[d.DeviceId]; !ok && d.DeviceId != "" {
				msg := expo.PushMessage{
					To:       []expo.ExponentPushToken{expo.ExponentPushToken(d.DeviceId)},
					Data:     userData,
					Title:    "Сообщения",
					Body:     "Пользователь отправил вам новое сообщение",
					Sound:    "default",
					Priority: expo.HighPriority,
				}
				if d.Platform == "ios" {
					msg.Badge = rcpt.To[uid].Unread
				}
				messages = append(messages, MessageData{Uid: uid, DeviceId: d.DeviceId, Message: msg})
			}
		}
	}

	// TODO fix group chat notifications
	/*if rcpt.Channel != "" {
		topic := rcpt.Channel
		userData := clonePayload(data)
		userData["topic"] = topic
		msg := fcm.Message{
			Topic: topic,
			Data:  userData,
			Notification: &fcm.Notification{
				Title: title,
				Body:  body,
			},
		}

		msg.Android = &fcm.AndroidConfig{
			Priority: "normal",
		}
		messages = append(messages, MessageData{Message: &msg})
	}*/

	return messages
}
