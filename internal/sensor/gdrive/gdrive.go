// Package gdrive reads how much space a Google account has left against its quota — Drive,
// Gmail and Photos together (docs/specs/services.md). The hub runs it for a service node.
package gdrive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/oauth2"

	"github.com/pravbeseda/monitor/internal/sensor"
)

const (
	tokenURL = "https://oauth2.googleapis.com/token"
	aboutURL = "https://www.googleapis.com/drive/v3/about"

	// maxBody bounds what is read of an answer: the quota is a few dozen bytes.
	maxBody = 1 << 16
)

// Credentials are the deployment's OAuth client and the refresh token the account granted it.
type Credentials struct {
	ClientID, ClientSecret, RefreshToken string
}

// String keeps the credentials out of any print of them or of what holds them.
func (c Credentials) String() string {
	return fmt.Sprintf("Google Drive credentials set: %t", c != Credentials{})
}

// Sensor is the gdrive sensor.
type Sensor struct {
	oauth    oauth2.Config
	refresh  string
	aboutURL string
	now      func() time.Time
}

// New builds the sensor for one account.
func New(c Credentials) *Sensor {
	return newSensor(c, tokenURL, aboutURL, time.Now)
}

func newSensor(c Credentials, tokenURL, aboutURL string, now func() time.Time) *Sensor {
	return &Sensor{
		oauth: oauth2.Config{
			ClientID:     c.ClientID,
			ClientSecret: c.ClientSecret,
			// Pinned: left unknown, a refused refresh is asked a second time another way.
			Endpoint: oauth2.Endpoint{TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInParams},
		},
		refresh:  c.RefreshToken,
		aboutURL: aboutURL,
		now:      now,
	}
}

// Name is the sensor's id in the configuration and the manifest.
func (s *Sensor) Name() string { return "gdrive" }

// Applicable is always true: the sensor is built only where its credentials are.
func (s *Sensor) Applicable() bool { return true }

// Collect refreshes an access token and reads the quota with it. The token lives no longer
// than the collection: one refresh an hour costs less than keeping it current in between.
func (s *Sensor) Collect(ctx context.Context) ([]sensor.Measurement, error) {
	token, err := s.oauth.TokenSource(ctx, &oauth2.Token{RefreshToken: s.refresh}).Token()
	if err != nil {
		return nil, refusal(s.oauth.Endpoint.TokenURL, err)
	}
	body, err := s.ask(ctx, token.AccessToken)
	if err != nil {
		return nil, err
	}
	free, limit, err := quota(body)
	if err != nil || limit == 0 {
		return nil, err
	}

	at := s.now()
	return []sensor.Measurement{
		{Metric: "gdrive.free_bytes", Value: float64(free), TS: at},
		{Metric: "gdrive.free_pct", Value: sensor.Round2(100 * float64(free) / float64(limit)), TS: at},
	}, nil
}

// refusal says what the token endpoint refused in words an operator acts on. The error is
// never wrapped: oauth2's carries the HTTP exchange, whose request holds the client's
// credentials.
func refusal(tokenURL string, err error) error {
	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		return unreachable(tokenURL, err)
	}
	switch retrieve.ErrorCode {
	case "invalid_grant":
		return errors.New("the refresh token was refused (invalid_grant): it expired or was revoked, so the account must be authorized again")
	case "invalid_client":
		return errors.New("the OAuth client's id or secret was refused (invalid_client)")
	case "unauthorized_client":
		return errors.New("the refresh token was issued to another client than the one configured (unauthorized_client)")
	case "":
		return fmt.Errorf("the Google token endpoint answered %d", retrieve.Response.StatusCode)
	default:
		return fmt.Errorf("the Google token endpoint answered %s", retrieve.ErrorCode)
	}
}

// unreachable names the host a call could not complete with and why, and nothing of the
// request itself.
func unreachable(endpoint string, err error) error {
	host := endpoint
	if u, parsed := url.Parse(endpoint); parsed == nil {
		host = u.Host
	}
	var failed *url.Error
	if errors.As(err, &failed) {
		return fmt.Errorf("reach %s: %v", host, failed.Err)
	}
	return fmt.Errorf("reach %s: no usable answer", host)
}

type driveError struct {
	Error struct {
		Errors []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

// ask fetches the account's quota with the access token.
func (s *Sensor) ask(ctx context.Context, accessToken string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.aboutURL+"?fields=storageQuota", nil)
	if err != nil {
		return nil, fmt.Errorf("build the Google Drive request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, unreachable(s.aboutURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("read the Google Drive answer: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var refused driveError
		reason := "no reason given"
		if json.Unmarshal(body, &refused) == nil && len(refused.Error.Errors) > 0 {
			reason = refused.Error.Errors[0].Reason
		}
		return nil, fmt.Errorf("the Drive API answered %d (%s)", resp.StatusCode, reason)
	}
	return body, nil
}

// quota reads the free space and the limit from Drive's answer; a limit of 0 says the
// account has none, since a limit Google writes as 0 is refused.
func quota(body []byte) (free, limit int64, err error) {
	var answer struct {
		StorageQuota *struct {
			Limit *string `json:"limit"`
			Usage *string `json:"usage"`
		} `json:"storageQuota"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return 0, 0, fmt.Errorf("the Google Drive quota is not the JSON expected: %w", err)
	}
	q := answer.StorageQuota
	switch {
	case q == nil:
		return 0, 0, errors.New("the Google Drive answer carries no storageQuota")
	case q.Limit == nil:
		return 0, 0, nil
	case q.Usage == nil:
		return 0, 0, errors.New("the Google Drive quota carries a limit and no usage")
	}
	if limit, err = count("limit", *q.Limit); err != nil {
		return 0, 0, err
	}
	if limit == 0 {
		return 0, 0, errors.New("the Google Drive quota has a limit of 0")
	}
	usage, err := count("usage", *q.Usage)
	if err != nil {
		return 0, 0, err
	}
	return max(limit-usage, 0), limit, nil
}

// count reads one figure of the quota, which Google writes as a string of digits.
func count(field, value string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("the Google Drive quota's %s %q is not a count of bytes", field, value)
	}
	return n, nil
}
