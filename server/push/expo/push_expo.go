package expo

import (
	"encoding/json"
	"errors"
	"fmt"
	expoSdk "github.com/oliveroneill/exponent-server-sdk-golang/sdk"
	"log"
	"os"

	"github.com/tinode/chat/server/push"
)

var handler Handler

const (
	// Size of the input channel buffer.
	bufferSize = 1024

	// The number of push messages sent in one batch. FCM constant.
	pushBatchSize = 100

	// The number of sub/unsub requests sent in one batch. FCM constant.
	subBatchSize = 1000

	// Maximum length of a text message in runes. The message is clipped if length is exceeded.
	// TODO: implement intelligent clipping of Drafty messages.
	maxMessageLength = 80
)

// Handler represents the push handler; implements push.PushHandler interface.
type Handler struct {
	input   chan *push.Receipt
	channel chan *push.ChannelReq
	stop    chan bool
	client  *expoSdk.PushClient
}

type configType struct {
	Enabled         bool            `json:"enabled"`
	Credentials     json.RawMessage `json:"credentials"`
	CredentialsFile string          `json:"credentials_file"`
	TimeToLive      uint            `json:"time_to_live,omitempty"`
}

// Init initializes the push handler
func (Handler) Init(jsonconf string) error {

	var config configType
	err := json.Unmarshal([]byte(jsonconf), &config)
	if err != nil {
		return errors.New("failed to parse config: " + err.Error())
	}

	if !config.Enabled {
		return nil
	}

	handler.client = expoSdk.NewPushClient(nil)
	handler.input = make(chan *push.Receipt, bufferSize)
	handler.channel = make(chan *push.ChannelReq, bufferSize)
	handler.stop = make(chan bool, 1)

	go func() {
		for {
			select {
			case rcpt := <-handler.input:
				go sendNotifications(rcpt)
			case sub := <-handler.channel:
				fmt.Fprintln(os.Stdout, sub)
			case <-handler.stop:
				return
			}
		}
	}()

	return nil
}

func sendNotifications(rcpt *push.Receipt) {
	messages := PrepareNotifications(rcpt)
	n := len(messages)
	if n == 0 {
		return
	}

	for i := 0; i < n; i += pushBatchSize {
		upper := i + pushBatchSize
		if upper > n {
			upper = n
		}
		var batch []expoSdk.PushMessage
		for j := i; j < upper; j++ {
			batch = append(batch, messages[j].Message)
		}
		resp, err := handler.client.PublishMultiple(batch)
		if err != nil {
			// Complete failure.
			log.Println("expo PublishMultiple error:", err)
			break
		}
		for i := 0; i < len(resp); i++ {
			if err = resp[i].ValidateResponse(); err != nil {
				if resp[i].Details["error"] == expoSdk.ErrorMessageRateExceeded {
					log.Println("expo PublishMultiple error:", resp[i].Details)
					return
				}
			}
		}
	}
}

// IsReady checks if the push handler has been initialized.
func (Handler) IsReady() bool {
	return handler.input != nil
}

// Push returns a channel that the server will use to send messages to.
// If the adapter blocks, the message will be dropped.
func (Handler) Push() chan<- *push.Receipt {
	return handler.input
}

// Channel returns a channel for subscribing/unsubscribing devices to FCM topics.
func (Handler) Channel() chan<- *push.ChannelReq {
	return handler.channel
}

// Stop shuts down the handler
func (Handler) Stop() {
	handler.stop <- true
}

func init() {
	push.Register("expo", &handler)
}
