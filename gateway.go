package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

type gateway struct {
	wsc *websocket.Conn
}

func newGateway(gatewayURL string) (*gateway, error) {
	wsc, _, err := websocket.DefaultDialer.Dial(gatewayURL, nil) // TODO: Specify API version
	if err != nil {
		return nil, fmt.Errorf("failed to dial websocket: %w", err)
	}
	return &gateway{
		wsc: wsc,
	}, nil
}

func (gw *gateway) Close() {
	gw.wsc.Close()
}

func (gw *gateway) ReadMessage() ([]byte, error) {
	_, message, err := gw.wsc.ReadMessage()
	return message, err
}

func (gw *gateway) writeJSONMessage(msg wsPayload) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	if msg.OP == 2 { // Identify, don't print token
		fmt.Printf("Send: opIdentify (censored)\n")
	} else {
		fmt.Printf("Send: %s\n", data)
	}
	if err := gw.wsc.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}
	return nil
}

type wsPayload struct {
	// https://ptb.discord.com/developers/docs/topics/opcodes-and-status-codes#gateway-gateway-opcodes
	OP  int    `json:"op"`          // opcode for the payload
	D   any    `json:"d"`           // event data
	Seq *int   `json:"s,omitempty"` // sequence number, used for resuming sessions and heartbeats
	T   string `json:"t,omitempty"` // the event name for this payload
}

func (wsp *wsPayload) UnmarshalJSON(data []byte) error {
	var v struct {
		OP  int             `json:"op"` // opcode for the payload
		D   json.RawMessage `json:"d"`  // event data
		Seq *int            `json:"s"`  // sequence number, used for resuming sessions and heartbeats
		T   string          `json:"t"`  // the event name for this payload
	}

	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("unmarhsaling json: %w", err)
	}
	wsp.OP = v.OP
	wsp.Seq = v.Seq
	wsp.T = v.T
	switch wsp.OP {
	case 0: // Dispatch
		switch wsp.T {
		case "READY":
			wsp.D = &dispatchReady{}
		case "MESSAGE_CREATE":
			wsp.D = &dispatchMessageCreate{}
		case "CHANNEL_CREATE":
			wsp.D = &dispatchChannelCreate{}
		default:
			return fmt.Errorf("parsing unknown Dispatch type %q", wsp.T)
		}
	case 1: // Hearbeat
		wsp.D = &opHeartbeat{}
	case 7:
		wsp.D = &opReconnect{}
	case 10: // Hello
		wsp.D = &opHello{}
	case 11: // Heartbeat ACK
		wsp.D = &opHeartbeatACK{}
	}
	if wsp.D != nil {
		if err := json.Unmarshal(v.D, wsp.D); err != nil {
			return fmt.Errorf("failed to parse Hello (opcode 10): %w", err)
		}
	}
	return nil
}

type opHello struct {
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
}

func (p *opHello) UnmarshalJSON(data []byte) error {
	var d map[string]interface{}
	if err := json.Unmarshal(data, &d); err != nil {
		return err
	}
	p.HeartbeatInterval = time.Duration(int(d["heartbeat_interval"].(float64))) * time.Millisecond
	return nil
}

type opHeartbeatACK struct{}

type opHeartbeat struct{}

type opReconnect struct{}

type opIdentify struct {
	Token           string             `json:"token"`           //	authentication token	-
	Properties      identifyProperties `json:"properties"`      //	connection properties	-
	Compres         bool               `json:"compress"`        // boolean	whether this connection supports compression of packets	false
	Large_threshold *int               `json:"large_threshold"` //dinteger	value between 50 and 250, total number of members where the gateway will stop sending offline members in the guild member list	50
	Shard           []int              `json:"shard,omitempty"` //darray of two integers (shard_id, num_shards)	used for Guild Sharding	-
	// Presence object`json:"presence"` //eupdate presence object	presence structure for initial presence information	-
	Intents int `json:"intents"` //	the Gateway Intents you wish to receive	-
}

type identifyProperties struct {
	OS      string `json:"$os"`
	Browser string `json:$browser"`
	Device  string `json:$device"`
}

type dispatchReady struct {
	V int `json:"v"` //	gateway version
	// User	user `json:"user"` //	object	information about the user including email
	// Guilds	array of Unavailable Guild objects	`json:"guilds"`// the guilds the user is in
	Session_id  string         `json:"session_id"`       //	used for resuming connections
	Shard       []int          `json:"shard",omitempty"` // array of two integers (shard_id, num_shards)	the shard information associated with this session, if sent when identifying
	Application applicationObj `json:"application"`      //	contains id and flags
}

// https://discord.com/developers/docs/topics/gateway#message-create
type dispatchMessageCreate = messageObj

type dispatchChannelCreate channelObj
