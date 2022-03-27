package main

import (
	"context"
	"fmt"
	"io"
	"regexp"

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

func parseMCLog(ctx context.Context, file string) (chan any, error) {
	out := make(chan any)

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
		return out, fmt.Errorf("tailing log file: %+v", err)
	}

	go func() {
		<-ctx.Done()
		t.Stop()
	}()

	go func() {
		defer t.Cleanup()
		// Print the text of each received line
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

	return out, nil
}
