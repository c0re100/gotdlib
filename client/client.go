package client

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync"
	"time"
)

var pendingUpdateType []Type

type Client struct {
	jsonClient      *JsonClient
	extraGenerator  ExtraGenerator
	responses       chan *Response
	pendingResp     chan *Response
	listenerStore   *listenerStore
	catchersStore   *sync.Map
	successMsgStore *sync.Map
	updatesTimeout  time.Duration
	catchTimeout    time.Duration
	DisablePatch    bool
}

type Option func(*Client)

func WithExtraGenerator(extraGenerator ExtraGenerator) Option {
	return func(client *Client) {
		client.extraGenerator = extraGenerator
	}
}

func WithCatchTimeout(timeout time.Duration) Option {
	return func(client *Client) {
		client.catchTimeout = timeout
	}
}

func WithProxy(req *AddProxyRequest) Option {
	return func(client *Client) {
		client.AddProxy(req)
	}
}

func WithoutSendMessagePatch() Option {
	return func(client *Client) {
		client.DisablePatch = true
	}
}

func SetLogLevel(level int32) {
	_, _ = SetLogVerbosityLevel(&SetLogVerbosityLevelRequest{
		NewVerbosityLevel: level,
	})
}

func SetFilePath(path string) {
	_, _ = SetLogStream(&SetLogStreamRequest{
		LogStream: &LogStreamFile{
			Path:           path,
			MaxFileSize:    10485760,
			RedirectStderr: true,
		},
	})
}

// Keep specific update type in memory when listener is not ready.
func SetPendingUpdateType(update ...Type) {
	for _, v := range update {
		pendingUpdateType = append(pendingUpdateType, v)
	}
}

func NewClient(authorizationStateHandler AuthorizationStateHandler, options ...Option) (*Client, error) {
	client := &Client{
		jsonClient:      NewJsonClient(),
		responses:       make(chan *Response, 1000),
		pendingResp:     make(chan *Response, 1000),
		listenerStore:   newListenerStore(),
		catchersStore:   &sync.Map{},
		successMsgStore: &sync.Map{},
	}

	client.extraGenerator = UuidV4Generator()
	client.catchTimeout = 60 * time.Second

	tdlibInstance.addClient(client)

	go client.processPendingResponse()
	go client.receiver()

	for _, option := range options {
		go option(client)
	}

	err := Authorize(client, authorizationStateHandler)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (client *Client) processResponse(response *Response) {
	if response.Extra != "" {
		value, ok := client.catchersStore.Load(response.Extra)
		if ok {
			value.(chan *Response) <- response
		}
	}

	typ, err := UnmarshalType(response.Data)
	if err != nil {
		return
	}

	if !client.DisablePatch && typ.GetType() == (&UpdateMessageSendSucceeded{}).GetType() {
		sendVal, sOk := client.successMsgStore.Load(typ.(*UpdateMessageSendSucceeded).OldMessageId)
		if sOk {
			sendVal.(chan *Response) <- response
		}
	}

	if len(client.listenerStore.Listeners()) == 0 {
		for _, p := range pendingUpdateType {
			if typ.GetType() == p.GetType() {
				client.pendingResp <- response
			}
		}
	}

	needGc := false
	for _, listener := range client.listenerStore.Listeners() {
		if listener.IsActive() && listener.Updates != nil && typ.GetType() == listener.Filter.GetType() { // All updates go to Updates channel if type == filter
			// Make some delay to UpdateMessageSendSucceeded listener
			// This can make UpdateMessageSendSucceeded response later than sendMessage response.
			// This may help a bot developer to map temporary message id to actual message id easily.
			// Cause an event listener slower than sendMessage response, so you have enough time to do mapping stuff.
			if typ.GetType() == (&UpdateMessageSendSucceeded{}).GetType() {
				go func(listener *Listener, typ Type) {
					time.Sleep(5 * time.Millisecond)
					listener.Updates <- typ
				}(listener, typ)
			} else {
				listener.Updates <- typ
			}
		} else if listener.IsActive() && listener.RawUpdates != nil { // All updates go to RawUpdates channel if filter is empty
			listener.RawUpdates <- typ
		} else if !listener.IsActive() { // GC inactive listener
			needGc = true
		}
	}
	if needGc {
		client.listenerStore.gc()
	}

	if typ.GetType() == TypeUpdateAuthorizationState && typ.(*UpdateAuthorizationState).AuthorizationState.AuthorizationStateType() == TypeAuthorizationStateClosed {
		close(client.responses)
	}
}

func (client *Client) receiver() {
	for response := range client.responses {
		client.processResponse(response)
	}
}

func (client *Client) processPendingResponse() {
	// No need to process pending response if no pending list.
	if len(pendingUpdateType) == 0 {
		return
	}

	// Wait for listener to be ready.
	for {
		if len(client.listenerStore.Listeners()) > 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Start processing pending response
	for response := range client.pendingResp {
		client.processResponse(response)
	}
}

func (client *Client) Send(req Request) (*Response, error) {
	req.Extra = client.extraGenerator()

	catcher := make(chan *Response, 1)

	client.catchersStore.Store(req.Extra, catcher)

	defer func() {
		client.catchersStore.Delete(req.Extra)
		close(catcher)
	}()

	client.jsonClient.Send(req)

	ctx, cancel := context.WithTimeout(context.Background(), client.catchTimeout)
	defer cancel()

	select {
	case response := <-catcher:
		if !client.DisablePatch && response.Type != "error" && req.Type == "sendMessage" {
			m, err := UnmarshalMessage(response.Data)
			if err != nil {
				return nil, err
			}

			if m.Content.MessageContentType() == "messageText" || m.Content.MessageContentType() == "messageDice" {
				successCatcher := make(chan *Response, 1)
				client.successMsgStore.Store(m.Id, successCatcher)

				defer (func() {
					client.successMsgStore.Delete(m.Id)
					close(successCatcher)
				})()

				select {
				case modResponse := <-successCatcher:
					m2, err2 := UnmarshalUpdateMessageSendSucceeded(modResponse.Data)
					if err2 != nil {
						return response, nil
					}
					response.Data = bytes.Replace(response.Data, []byte("\"@type\":\"messageSendingStatePending\""), []byte("\"@type\":\"updateMessageSendSucceeded\""), 1)
					response.Data = bytes.Replace(response.Data, []byte("\"id\":"+strconv.FormatInt(m.Id, 10)), []byte("\"id\":"+strconv.FormatInt(m2.Message.Id, 10)), 1)
					return response, nil
				case <-time.After(1 * time.Second):
					return response, nil
				}
			}
		}
		return response, nil
	case <-ctx.Done():
		return nil, errors.New("response catching timeout")
	}
}

func (client *Client) GetListener() *Listener {
	listener := &Listener{
		isActive:   true,
		RawUpdates: make(chan Type, 1000),
	}
	client.listenerStore.Add(listener)

	return listener
}

func (client *Client) AddEventReceiver(msgType Type, channelCapacity int) *Listener {
	listener := &Listener{
		isActive: true,
		Updates:  make(chan Type, channelCapacity),
		Filter:   msgType,
	}
	client.listenerStore.Add(listener)

	return listener
}
