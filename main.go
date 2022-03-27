package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	reqEnvs := []string{
		"DISCRAFT_TOKEN",
		"DISCRAFT_CHANNEL",
	}
	for _, env := range reqEnvs {
		if os.Getenv(env) == "" {
			fmt.Printf("Please set these environment variables: %s\n", strings.Join(reqEnvs, ", "))
			fmt.Printf("%s is not set\n", env)
			os.Exit(10) // exit-status 10 means the service will not restart
		}
	}

	restClient := newRESTClient()

	gatewayURL, err := restClient.getGatewayURL()
	if err != nil {
		panic(err)
	}

	fmt.Printf("Connecting to Gateway URL = %+v\n", gatewayURL)

	gw, err := newGateway(gatewayURL)
	if err != nil {
		panic(err)
	}
	defer gw.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		discordMain(gw, restClient)
		wg.Done()
	}()
	go func() {
		mcMain(context.Background(), gw, restClient)
		wg.Done()
	}()

	wg.Wait()
}

func discordMain(gw *gateway, restClient *restClient) {
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
		case *opReconnect:
			fmt.Println("Recieve: Reconnect")
			fmt.Println("Shutting down..")
			os.Exit(0)
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
					msg, err := restClient.createMessage(d.ChannelID, "pong")
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

const mcLogFile = "/tmp/latest.log"

func mcMain(ctx context.Context, gw *gateway, restClient *restClient) {
	mcChannelID := snowflake(os.Getenv("DISCRAFT_CHANNEL"))
	if len(mcChannelID) == 0 {
		panic("DISCRAFT_CHANNEL not set")
	}

	lines, err := parseMCLog(ctx, mcLogFile)
	if err != nil {
		panic(err)
	}

	for log := range lines {
		switch l := log.(type) {
		case logJoin:
			if _, err := restClient.createMessage(mcChannelID, fmt.Sprintf("%s joined", l.user)); err != nil {
				fmt.Printf("failed to create message for %#v: %+v", l, err)
			}
		case logPart:
			if _, err := restClient.createMessage(mcChannelID, fmt.Sprintf("%s left", l.user)); err != nil {
				fmt.Printf("failed to create message for %#v: %+v", l, err)
			}
		case logMsg:
			if _, err := restClient.createMessage(mcChannelID, fmt.Sprintf("<%s> %s", l.user, l.msg)); err != nil {
				fmt.Printf("failed to create message for %#v: %+v", l, err)
			}
		default:
			fmt.Printf("Unsupported mc log of type %T: %+v", l, l)
		}
	}
}
