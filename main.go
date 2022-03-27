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
)

const discordBaseURL = "https://discord.com/api"

const (
	INTENT_GUILD_MESSAGES  = 1 << 9
	INTENT_DIRECT_MESSAGES = 1 << 12
)

func main() {
	gatewayURL, err := getGatewayURL()
	if err != nil {
		panic(err)
	}

	fmt.Printf("Connecting to Gateway URL = %+v\n", gatewayURL)

	gw, err := newGateway(gatewayURL)
	if err != nil {
		panic(err)
	}
	defer gw.Close()
	discordMain(gw)
}

func discordMain(gw *gateway) {
	var initialStartup sync.Once
	var myID snowflake

	for {
		message, err := gw.ReadMessage()
		if err != nil {
			panic(err)
		}
		payload := &wsPayload{}
		if err := json.Unmarshal(message, payload); err != nil {
			fmt.Printf("Failed to parse message (follows); err is %+v\n\t%s\n", payload, message)
			continue
		}

		switch d := payload.D.(type) {
		case *opHello:
			heartbeatInterval := d.HeartbeatInterval
			fmt.Printf("Recieve: Hello Payload with HeartbeatInterval %v\n", heartbeatInterval)
			initialStartup.Do(func() {
				go func() {
					for {
						time.Sleep(heartbeatInterval)
						if err := gw.writeJSONMessage(wsPayload{
							OP: 1,
						}); err != nil {
							panic(err)
						}
					}
				}()
				if err := gw.writeJSONMessage(wsPayload{
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
		case *opHeartbeatACK:
			fmt.Println("Recieve: Heartbeat ACK")
		case *dispatchReady:
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
				striped := d.Content
				striped = strings.ReplaceAll(striped, fmt.Sprintf("<@!%s>", myID), "")
				striped = strings.ReplaceAll(striped, fmt.Sprintf("<@&%s>", myID), "")
				striped = strings.ReplaceAll(striped, fmt.Sprintf("<@%s>", myID), "")
				striped = strings.TrimSpace(striped)
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
					fmt.Printf("The stripped content was '%s'\n", striped)
				}
			}
		case *dispatchChannelCreate:
			fmt.Printf("Recieve Dispatch: CHANNEL_CREATE: %+v\n", d)
		default:
			fmt.Printf("Recieve: Unsupported payload: %+v\nD: %+v\n", payload, payload.D)
		}
	}
}

func doReq(req *http.Request) (*http.Response, error) {
	client := &http.Client{}

	req.Header.Add("Authorization", "Bot "+os.Getenv("DISCRAFT_TOKEN"))

	return client.Do(req)
}

// https://ptb.discord.com/developers/docs/topics/gateway#get-gateway-bot
type gatewayResp struct {
	URL string `json:"url"`
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
