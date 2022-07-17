package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
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
		"DISCRAFT_MCLOGFILE",
		"DISCRAFT_MCHOST",
		"DISCRAFT_MCPORT",
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

	mcServer := newMCServer(gw, restClient)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		discordMain(gw, restClient, mcServer)
		wg.Done()
	}()
	go func() {
		mcServer.run(context.Background())
		wg.Done()
	}()

	wg.Wait()
}

func discordMain(gw *gateway, restClient *restClient, mcServer *mcServer) {
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
				case "playing?":
					players := mcServer.getPlayers()
					var reply string
					if len(players) == 0 {
						reply = "No one is playing :("
					} else if len(players) == 1 {
						reply = fmt.Sprintf("Currently %s is playing alone", players[0])
					} else {
						reply = fmt.Sprintf("Currently %s and %s are playing", strings.Join(players[1:], ", "), players[0])
					}
					msg, err := restClient.createMessage(d.ChannelID, reply)
					if err != nil {
						fmt.Printf("Failed to respond to playing: %+v", err)
						return
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

type mcServer struct {
	sync.Mutex
	players      map[string]struct{}
	latestStatus string

	restClient *restClient
	gw         *gateway
	channelID  snowflake
}

func (serv *mcServer) playerJoined(player string) {
	serv.Lock()
	defer serv.Unlock()
	serv.players[player] = struct{}{}
}

func (serv *mcServer) playerParted(player string) {
	serv.Lock()
	defer serv.Unlock()
	delete(serv.players, player)
}

func (serv *mcServer) setPlayers(players []string) {
	serv.Lock()
	defer serv.Unlock()
	serv.players = map[string]struct{}{}
	for _, player := range players {
		serv.players[player] = struct{}{}
	}
}

func (serv *mcServer) getPlayers() []string {
	serv.Lock()
	defer serv.Unlock()
	players := []string{}
	for player := range serv.players {
		players = append(players, player)
	}
	sort.Strings(players)
	return players
}

func newMCServer(gw *gateway, restClient *restClient) *mcServer {
	mcChannelID := snowflake(os.Getenv("DISCRAFT_CHANNEL"))
	if len(mcChannelID) == 0 {
		panic("DISCRAFT_CHANNEL not set")
	}

	return &mcServer{
		players:    map[string]struct{}{},
		channelID:  mcChannelID,
		restClient: restClient,
		gw:         gw,
	}
}

func (serv *mcServer) updateStatus() {
	players := serv.getPlayers()
	status := "is none" // will be displayed as "Playing is none"
	if len(players) > 0 {
		status = fmt.Sprintf("is %d players", len(players))
	}

	serv.setStatus(status)
}

func (serv *mcServer) setStatus(status string) {
	if status == serv.latestStatus {
		return
	}
	serv.latestStatus = status

	if err := serv.gw.writeJSONMessage(wsPayload{
		OP: 3,
		D: opUpdatePresence{
			Since:  int(time.Now().Unix()),
			Status: "idle",
			Activities: []activityObject{
				{Type: 0, Name: status},
			},
		},
	}); err != nil {
		panic(err)
	}
}

func (serv *mcServer) run(ctx context.Context) {
	mcPort, err := strconv.ParseUint(os.Getenv("DISCRAFT_MCPORT"), 10, 16)
	if err != nil {
		panic(err)
	}

	lines, err := monitorMCServer(ctx, os.Getenv("DISCRAFT_MCLOGFILE"), os.Getenv("DISCRAFT_MCHOST"), uint16(mcPort))
	if err != nil {
		panic(err)
	}

	for log := range lines {
		switch l := log.(type) {
		case logJoin:
			serv.playerJoined(l.user)
			serv.updateStatus()
		case logPart:
			serv.playerParted(l.user)
			serv.updateStatus()
		case logMsg:
			if _, err := serv.restClient.createMessage(serv.channelID, fmt.Sprintf("<%s> %s", l.user, l.msg)); err != nil {
				fmt.Printf("failed to create message for %#v: %+v", l, err)
			}
		case logCorruption:
			if _, err := serv.restClient.createMessage(serv.channelID, "Corruption detected in log. Someone probably needs to restore a backup!"); err != nil {
				fmt.Printf("failed to create message for %#v: %+v", l, err)
			}
		case mcPing:
			serv.setPlayers(l.players)
			serv.updateStatus()
		case mcError:
			serv.setStatus("is none because ping failed")
		default:
			fmt.Printf("Unsupported mc log of type %T: %+v", l, l)
		}
	}
}
