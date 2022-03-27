package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const discordBaseURL = "https://discord.com/api"

type wsPayload struct {
	// https://ptb.discord.com/developers/docs/topics/opcodes-and-status-codes#gateway-gateway-opcodes
	OP  int    `json:"op"`          // opcode for the payload
	D   any    `json:"d"`           // event data
	Seq *int   `json:"s,omitempty"` // sequence number, used for resuming sessions and heartbeats
	T   string `json:"t,omitempty"` // the event name for this payload
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

const (
	INTENT_GUILD_MESSAGES  = 1 << 9
	INTENT_DIRECT_MESSAGES = 1 << 12
)

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
	Session_id  string         `json:"session_id"`       //	used for resuming connections
	Shard       []int          `json:"shard",omitempty"` // array of two integers (shard_id, num_shards)	the shard information associated with this session, if sent when identifying
	Application applicationObj `json:"application"`      //	contains id and flags
}

type applicationObj struct {
	ID    snowflake `json:"id"`
	Flags int       `json:"flags"`
}

type snowflake string

// https://discord.com/developers/docs/topics/gateway#message-create
type dispatchMessageCreate = messageObj

// https://discord.com/developers/docs/resources/channel#message-object
type messageObj struct {
	ID        snowflake  `json:"id"`         // id of the message
	ChannelID snowflake  `json:"channel_id"` // id of the channel the message was sent in
	GuildID   *snowflake `json:"guild_id"`   // id of the guild the message was sent in
	Author    *userObj   `json:"author"`     // the author of this message (not guaranteed to be a valid user, see below)
	// Member	partial guild member object	`json:"member?**"`	// member properties for this message's author
	Content string `json:"content"` // contents of the message
	// Timestamp	ISO8601 timestamp	`json:"timestamp"`	// when this message was sent
	// EditedTimestamp	?ISO8601 timestamp	`json:"edited_timestamp"`	// when this message was edited (or null if never)
	TTS             bool      `json:"tts"`              // whether this was a TTS message
	MentionEveryone bool      `json:"mention_everyone"` // whether this message mentions everyone
	Mentions        []userObj `json:"mentions"`         // users specifically mentioned in the message
	// MentionRoles	array of role object ids	`json:"mention_roles"`	// roles specifically mentioned in this message
	// MentionChannels	array of channel mention objects	`json:"mention_channels?****"`	// channels specifically mentioned in this message
	// Attachments	array of attachment objects	`json:"attachments"`	// any attached files
	// Embeds	array of embed objects	`json:"embeds"`	// any embedded content
	// Reactions	array of reaction objects	`json:"reactions?"`	// reactions to the message
	Nonce     string     `json:"nonce"`      // used for validating a message was sent
	Pinned    bool       `json:"pinned"`     // whether this message is pinned
	WebhookID *snowflake `json:"webhook_id"` // if the message is generated by a webhook, this is the webhook's id
	Type      int        `json:"type"`       // type of message
	// Activity	*message activity object	`json:"activity"`	// sent with Rich Presence-related chat embeds
	// Application	*partial application object	`json:"application"`	// sent with Rich Presence-related chat embeds
	ApplicationID *snowflake `json:"application_id"` // if the message is an Interaction or application-owned webhook, this is the id of the application
	// MessageReference	*message reference object	`json:"message_reference"`	// data showing the source of a crosspost, channel follow add, pin, or reply message
	Flags int `json:"flags"` // message flags combined as a bitfield
	// ReferencedMessage	message object	`json:"referenced_message?*****"`	// the message associated with the message_reference
	// Interaction	*message interaction object	`json:"interaction"`	// sent if the message is a response to an Interaction
	// Thread	*channel object	`json:"thread"`	// the thread that was started from this message, includes thread member object
	// Components	*Array of message components	`json:"components"`	// sent if the message contains components like buttons, action rows, or other interactive components
	// StickerItems	*array of message sticker item objects	`json:"sticker_items"`	// sent if the message contains stickers
	// Stickers?	array of sticker objects	`json:"stickers?"`	// Deprecated the stickers sent with the message
}

type userObj struct {
	ID            snowflake `json:"id"`            // the user's id
	Username      string    `json:"username"`      // the user's username, not unique across the platform
	Discriminator string    `json:"discriminator"` // the user's 4-digit discord-tag
	Avatar        string    `json:"avatar"`        // the user's avatar hash
	Bot           *bool     `json:"bot"`           // whether the user belongs to an OAuth2 application
	System        *bool     `json:"system"`        // whether the user is an Official Discord System user (part of the urgent message system)
	MFAEnabled    *bool     `json:"mfa_enabled"`   // whether the user has two factor enabled on their account
	Banner        string    `json:"banner"`        // the user's banner hash
	AccentColor   *int      `json:"accent_color"`  // the user's banner color encoded as an integer representation of hexadecimal color code
	Locale        string    `json:"locale"`        // the user's chosen language option
	Verified      *bool     `json:"verified"`      // whether the email on this account has been verified
	Email         string    `json:"email"`         // the user's email
	Flags         *int      `json:"flags"`         // the flags on a user's account
	PremiumType   *int      `json:"premium_type"`  // the type of Nitro subscription on a user's account
	PublicFlags   *int      `json:"public_flags"`  // the public flags on a user's account
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
			wsp.D = &opReady{}
		case "MESSAGE_CREATE":
			wsp.D = &dispatchMessageCreate{}
		case "CHANNEL_CREATE":
			wsp.D = &dispatchChannelCreate{}
		default:
			return fmt.Errorf("parsing unknown Dispatch type %q", wsp.T)
		}
	case 1: // Hearbeat
		wsp.D = &heartbeatPayload{}
	case 10: // Hello
		wsp.D = &helloPayload{}
	case 11: // Heartbeat ACK
		wsp.D = &heartbeatACK{}
	}
	if wsp.D != nil {
		if err := json.Unmarshal(v.D, wsp.D); err != nil {
			return fmt.Errorf("failed to parse Hello (opcode 10): %w", err)
		}
	}
	return nil
}

type dispatchChannelCreate channelObj

// https://discord.com/developers/docs/resources/channel#channel-object
type channelObj struct {
	ID   snowflake `json:"id"`   // the id of this channel
	Name string    `json:"name"` // the name of the channel (1-100 characters)
}

func main() {
	gatewayURL, err := getGatewayURL()
	if err != nil {
		panic(err)
	}

	fmt.Printf("Connecting to Gateway URL = %+v\n", gatewayURL)

	wsc, _, err := websocket.DefaultDialer.Dial(gatewayURL, nil) // TODO: Specify API version
	if err != nil {
		panic(err)
	}
	defer wsc.Close()

	var heartbeatOnce sync.Once // TODO rename to initialStartup?

	writeJSONMessage := func(msg any) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("marshaling message: %w", err)
		}
		fmt.Printf("Send: %s\n", data)
		if err := wsc.WriteMessage(websocket.TextMessage, data); err != nil {
			return fmt.Errorf("writing message: %w", err)
		}
		return nil
	}

	var myID snowflake

	for {
		_, message, err := wsc.ReadMessage()
		if err != nil {
			panic(err)
		}
		payload := &wsPayload{}
		if err := json.Unmarshal(message, payload); err != nil {
			fmt.Printf("Failed to parse message (follows); err is %+v\n\t%s\n", payload, message)
			continue
		}

		switch d := payload.D.(type) {
		case *helloPayload:
			heartbeatInterval := d.HeartbeatInterval
			heartbeatOnce.Do(func() {
				fmt.Printf("Recieve: Hello Payload with HeartbeatInterval %v\n", heartbeatInterval)
				go func() {
					for {
						time.Sleep(heartbeatInterval)
						if err := writeJSONMessage(wsPayload{
							OP: 1,
						}); err != nil {
							panic(err)
						}
					}
				}()
				if err := writeJSONMessage(wsPayload{
					OP: 2,
					D: opIdentify{
						Token: os.Getenv("DISCRAFT_TOKEN"),
						Properties: identifyProperties{
							OS:      "linux",
							Browser: "discraft",
							Device:  "discraft",
						},
						Intents: INTENT_GUILD_MESSAGES | INTENT_DIRECT_MESSAGES,
					},
				}); err != nil {
					panic(err)
				}
			})
		case *heartbeatACK:
			fmt.Println("Recieve: Heartbeat ACK")
		case *opReady:
			myID = d.Application.ID
			fmt.Printf("Recieve: Ready: %+v\n", d)
			fmt.Printf("myID = %+v\n", myID)
		case *dispatchMessageCreate:
			fmt.Printf("Recieve Dispatch: MESSAGE_CREATE = <%s> %s\n", d.Author.Username, d.Content)
			mentionsMe := false
			for _, mention := range d.Mentions {
				if mention.ID == myID {
					mentionsMe = true
					break
				}
			}
			if mentionsMe {
				striped := strings.TrimSpace(strings.ReplaceAll(d.Content, fmt.Sprintf("<@!%s>", myID), ""))
				switch strings.ToLower(striped) {
				case "ping":
					msg, err := createMessage(d.ChannelID, "pong")
					if err != nil {
						fmt.Printf("Failed to create message: %+v", err)
						break
					}
					fmt.Printf("msg = %+v\n", msg)
				default:
					fmt.Println("This message was for me but I didn't know what to do")
				}
			}
		case *dispatchChannelCreate:
			fmt.Printf("Recieve Dispatch: CHANNEL_CREATE: %+v\n", d)
		default:
			fmt.Printf("Recieve: Unsupported payload: %+v\nD: %+v\n", payload, payload.D)
		}
	}
}

// https://ptb.discord.com/developers/docs/topics/gateway#get-gateway-bot
type gatewayResp struct {
	URL string `json:"url"`
}

func doReq(req *http.Request) (*http.Response, error) {
	client := &http.Client{}

	req.Header.Add("Authorization", "Bot "+os.Getenv("DISCRAFT_TOKEN"))

	return client.Do(req)
}

func getGatewayURL() (string, error) {
	req, err := http.NewRequest("GET", discordBaseURL+"/gateway/bot", nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	res, err := doReq(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer res.Body.Close()

	// TODO check res.Status

	dec := json.NewDecoder(res.Body)
	gResp := &gatewayResp{}
	if err := dec.Decode(gResp); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return gResp.URL, nil
}

// https://discord.com/developers/docs/resources/channel#create-message
func createMessage(channel snowflake, content string) (*messageObj, error) {
	createMSGURL := fmt.Sprintf("%s/channels/%s/messages", discordBaseURL, channel)

	data, err := json.Marshal(map[string]interface{}{
		"content": content,
	})

	if err != nil {
		return nil, fmt.Errorf("marshaling JSON: %w", err)
	}

	req, err := http.NewRequest("POST", createMSGURL, io.NopCloser(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Add("content-type", "application/json")

	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	res, err := doReq(req)
	if err != nil {
		return nil, fmt.Errorf("doing request: %w", err)
	}
	defer res.Body.Close()

	fmt.Printf("res = %+v\n", res)
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading entire response body: %w", err)
	}
	msg := &messageObj{}
	if err := json.Unmarshal(body, msg); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return msg, nil
}
