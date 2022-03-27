package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type restClient struct {
	sync.Mutex // protect the limits
	client     *http.Client
	limits     map[string]rateLimit
}

type rateLimit struct {
	resets    time.Time
	remaining int
}

func (l *rateLimit) Wait() {
	if l == nil {
		return
	}
	if l.remaining == 0 {
		waitFor := l.resets.Sub(time.Now())
		fmt.Printf("Hit rate limit, waiting for %v", waitFor)
		time.Sleep(waitFor)
	}
}

func newRESTClient() *restClient {
	return &restClient{
		client: &http.Client{},
		limits: map[string]rateLimit{},
	}
}

func (rc *restClient) doReq(req *http.Request) (*http.Response, error) {
	req.Header.Add("Authorization", "Bot "+os.Getenv("DISCRAFT_TOKEN"))
	rc.getLimit(req).Wait()
	res, err := rc.client.Do(req)
	rc.updateLimits(res)
	return res, err
}

func getBucket(req *http.Request) string {
	// bucket := res.Header.Get("X-RateLimit-Bucket") // This bucket is not very useful when we can't calculate it ourselves. Lets just use a good guess on what it depends on
	return req.Method + " " + req.URL.String()
}

func (rc *restClient) getLimit(req *http.Request) *rateLimit {
	rc.Lock()
	defer rc.Unlock()
	if limit, ok := rc.limits[getBucket(req)]; ok {
		return &limit
	}
	return nil
}

func (rc *restClient) updateLimits(res *http.Response) {
	if res == nil {
		return
	}
	remaining, err := strconv.Atoi(res.Header.Get("X-RateLimit-Remaining"))
	if err != nil {
		fmt.Printf("Failed to parse X-RateLimit-Remaining header: %#v", err)
		return
	}
	resetEpoch, err := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		fmt.Printf("Failed to parse X-RateLimit-Reset header: %#v", err)
		return
	}
	resetTime := time.Unix(resetEpoch, 0)

	rc.Lock()
	defer rc.Unlock()

	rc.limits[getBucket(res.Request)] = rateLimit{
		resets:    resetTime,
		remaining: remaining,
	}
}

// https://ptb.discord.com/developers/docs/topics/gateway#get-gateway-bot
type gatewayResp struct {
	URL string `json:"url"`
}

func (rc *restClient) getGatewayURL() (string, error) {
	req, err := http.NewRequest("GET", discordBaseURL+"/gateway/bot", nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	res, err := rc.doReq(req)
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
func (rc *restClient) createMessage(channel snowflake, content string) (*messageObj, error) {
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

	res, err := rc.doReq(req)
	if err != nil {
		return nil, fmt.Errorf("doing request: %w", err)
	}
	defer res.Body.Close()

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
