package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type restClient struct {
	client *http.Client
}

func newRESTClient() *restClient {
	return &restClient{
		client: &http.Client{},
	}
}

func (rc *restClient) doReq(req *http.Request) (*http.Response, error) {
	req.Header.Add("Authorization", "Bot "+os.Getenv("DISCRAFT_TOKEN"))
	return rc.client.Do(req)
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
