package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kira1928/bililive-scheduler/internal/model"
)

type BiliAPI struct {
	baseURL    string
	httpClient *http.Client
}

func NewBiliAPI(baseURL string) *BiliAPI {
	baseURL = strings.TrimRight(baseURL, "/")
	return &BiliAPI{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type liveInfo struct {
	ID            string `json:"id"`
	HostName      string `json:"host_name"`
	RoomName      string `json:"room_name"`
	RawURL        string `json:"raw_url"`
	Status        bool   `json:"status"`
	Listening     bool   `json:"listening"`
	Recording     bool   `json:"recording"`
	Initializing  bool   `json:"initializing"`
}

type commonResp struct {
	ErrNo  int    `json:"error"`
	ErrMsg string `json:"error_msg"`
}

func (c *BiliAPI) GetRooms(ctx context.Context) ([]model.RoomInfo, error) {
	var lives []liveInfo
	if err := c.getJSON(ctx, "/api/lives", &lives); err != nil {
		return nil, err
	}

	rooms := make([]model.RoomInfo, 0, len(lives))
	for _, l := range lives {
		rooms = append(rooms, model.RoomInfo{
			ID:       l.ID,
			HostName: l.HostName,
			RoomName: l.RoomName,
			URL:      l.RawURL,
			IsLive:   l.Status,
		})
	}
	return rooms, nil
}

func (c *BiliAPI) GetRoomStatus(ctx context.Context, roomID string) (isLive, isRecording bool, err error) {
	var info liveInfo
	if err := c.getJSON(ctx, "/api/lives/"+roomID, &info); err != nil {
		return false, false, err
	}
	return info.Status, info.Recording, nil
}

func (c *BiliAPI) StartRecording(ctx context.Context, roomID string) error {
	return c.postAction(ctx, roomID, "start")
}

func (c *BiliAPI) StopRecording(ctx context.Context, roomID string) error {
	return c.postAction(ctx, roomID, "stop")
}

func (c *BiliAPI) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/info", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *BiliAPI) postAction(ctx context.Context, roomID, action string) error {
	url := fmt.Sprintf("%s/api/lives/%s/%s", c.baseURL, roomID, action)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("action %s returned status %d: %s", action, resp.StatusCode, string(body))
	}
	return nil
}

func (c *BiliAPI) getJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s returned status %d: %s", path, resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
