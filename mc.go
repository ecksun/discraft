package main

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"time"

	"github.com/alteamc/minequery/ping"
	"github.com/nxadm/tail"
)

type logJoin struct {
	user string
}

type logPart struct {
	user string
}

type logMsg struct {
	user string
	msg  string
}

type mcPing struct {
	players []string
}

func monitorMCServer(ctx context.Context, file string, mcHost string, mcPort uint16) (chan any, error) {
	out := make(chan any)
	if err := parseMCLog(ctx, out, file); err != nil {
		return out, fmt.Errorf("failed to start log file parsing: %w", err)
	}
	pingMCServer(ctx, out, mcHost, mcPort)

	return out, nil
}

func parseMCLog(ctx context.Context, out chan any, file string) error {
	timeRegex := `[0-2][0-9]:[0-6][0-9]:[0-6][0-9]`
	joinRegex := regexp.MustCompile(fmt.Sprintf(`\[%s\] \[Server thread\/INFO\]: (.*) joined the game`, timeRegex))
	partRegex := regexp.MustCompile(fmt.Sprintf(`\[%s\] \[Server thread\/INFO\]: (.*) left the game`, timeRegex))
	msgRegex := regexp.MustCompile(fmt.Sprintf(`\[%s\] \[Server thread\/INFO\]: <([^>]+)> (.*)`, timeRegex))

	t, err := tail.TailFile(file, tail.Config{
		Follow: true,
		ReOpen: true,
		Location: &tail.SeekInfo{
			Whence: io.SeekEnd,
		},
	})
	if err != nil {
		return fmt.Errorf("tailing log file: %+v", err)
	}

	go func() {
		<-ctx.Done()
		t.Stop()
	}()

	go func() {
		defer t.Cleanup()
		for line := range t.Lines {
			if line.Text == "EOF for testing" {
				break
			}
			if match := joinRegex.FindStringSubmatch(line.Text); len(match) > 0 {
				out <- logJoin{
					user: match[1],
				}
			}
			if match := partRegex.FindStringSubmatch(line.Text); len(match) > 0 {
				out <- logPart{
					user: match[1],
				}
			}
			if match := msgRegex.FindStringSubmatch(line.Text); len(match) > 0 {
				out <- logMsg{
					user: match[1],
					msg:  match[2],
				}
			}
		}
		close(out)
	}()

	return nil
}

func pingMCServer(ctx context.Context, out chan any, host string, port uint16) {
	pingServer := func() {
		res, err := ping.Ping(host, port)
		if err != nil {
			panic(err)
		}
		players := []string{}
		for _, player := range res.Players.Sample {
			players = append(players, player.Name)
		}

		sort.Strings(players)

		out <- mcPing{
			players: players,
		}
	}

	go func() {
		pingServer()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Minute):
				pingServer()
			}
		}
	}()
}
