package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const discordBaseURL = "https://discord.com/api"

type wsPayload struct {
	// https://ptb.discord.com/developers/docs/topics/opcodes-and-status-codes#gateway-gateway-opcodes
	OP  int    `json:"op"` // opcode for the payload
	D   any    `json:"d"`  // event data
	Seq *int   `json:"s"`  // sequence number, used for resuming sessions and heartbeats
	T   string `json:"t"`  // the event name for this payload
}

type helloPayload struct {
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
}

func (p *helloPayload) UnmarshalJSON(data []byte) error {
	var d map[string]interface{}
	if err := json.Unmarshal(data, &d); err != nil {
		return err
	}
	p.HeartbeatInterval = time.Duration(int(d["heartbeat_interval"].(float64))) * time.Millisecond
	return nil
}

type heartbeatACK struct{}

type heartbeatPayload struct{}

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

type opReady struct {
	V int `json:"v"` //	gateway version
	// User	user `json:"user"` //	object	information about the user including email
	// Guilds	array of Unavailable Guild objects	`json:"guilds"`// the guilds the user is in
	// Session_id  string         `json:"session_id"`       //	used for resuming connections
	// Shard       []int          `json:"shard",omitempty"` // array of two integers (shard_id, num_shards)	the shard information associated with this session, if sent when identifying
	Application applicationObj `json:"application"` //	contains id and flags
}

type applicationObj struct {
	ID    string `json:"id"`
	Flags int    `json:"flags"`
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
	case 0:
		wsp.D = &opReady{}
	case 1:
		wsp.D = &heartbeatPayload{}
	case 10:
		wsp.D = &helloPayload{}
	case 11:
		wsp.D = &heartbeatACK{}
	}
	if wsp.D != nil {
		if err := json.Unmarshal(v.D, wsp.D); err != nil {
			return fmt.Errorf("failed to parse Hello (opcode 10): %w", err)
		}
	}
	return nil
}

func main() {
	gatewayURL, err := getGatewayURL()
	if err != nil {
		panic(err)
	}

	fmt.Printf("gatewayURL = %+v\n", gatewayURL)

	wsc, _, err := websocket.DefaultDialer.Dial(gatewayURL, nil) // TODO: Specify API version
	if err != nil {
		panic(err)
	}
	defer wsc.Close()

	var heartbeatOnce sync.Once // TODO rename to initialStartup?

	for {
		_, message, err := wsc.ReadMessage()
		if err != nil {
			panic(err)
		}
		payload := &wsPayload{}
		if err := json.Unmarshal(message, payload); err != nil {
			panic(err)
		}

		switch d := payload.D.(type) {
		case *helloPayload:
			heartbeatInterval := d.HeartbeatInterval
			heartbeatOnce.Do(func() {
				fmt.Printf("Got Hello Payload with HeartbeatInterval %v\n", heartbeatInterval)
				go func() {
					for {
						time.Sleep(heartbeatInterval)
						fmt.Println("Sending heartbeat")
						err := wsc.WriteMessage(websocket.TextMessage, []byte(`{ "op": 1, "d": {} }`))
						if err != nil {
							panic(err)
						}
						return
					}
				}()
				jsond, err := json.Marshal(wsPayload{
					OP: 2,
					D: opIdentify{
						Token: os.Getenv("DISCRAFT_TOKEN"),
						Properties: identifyProperties{
							OS:      "linux",
							Browser: "discraft",
							Device:  "discraft",
						},
					},
				})
				if err != nil {
					panic(err)
				}

				fmt.Printf("jsond = %+v\n", string(jsond))
				if err := wsc.WriteMessage(websocket.TextMessage, jsond); err != nil {
					panic(err)
				}
			})
		case *heartbeatACK:
			fmt.Println("Heartbeat ACK")
		case *opReady:
			fmt.Printf("Ready: %+v\n", d)
		default:
			fmt.Printf("Unsupported payload: %+v\nD: %+v\n", payload, payload.D)
		}
	}
}

// https://ptb.discord.com/developers/docs/topics/gateway#get-gateway-bot
type gatewayResp struct {
	URL string `json:"url"`
}

func getGatewayURL() (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", discordBaseURL+"/gateway/bot", nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Add("Authorization", "Bot "+os.Getenv("DISCRAFT_TOKEN"))
	fmt.Printf("req.Header = %+v\n", req.Header)

	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer res.Body.Close()

	fmt.Printf("res.Status = %+v\n", res.Status)

	dec := json.NewDecoder(res.Body)
	gResp := &gatewayResp{}
	if err := dec.Decode(gResp); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	fmt.Printf("gResp = %+v\n", gResp)
	return gResp.URL, nil
}
