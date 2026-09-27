package gdrive

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
)

// Synthetic credentials (ADR 0007).
var credentials = Credentials{
	ClientID:     "synthetic-client-id",
	ClientSecret: "synthetic-client-secret",
	RefreshToken: "synthetic-refresh-token",
}

const accessToken = "synthetic-access-token"

var collected = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

// google stands in for both of Google's endpoints: the token endpoint answers token, and the
// Drive endpoint answers drive, each a handler the test chooses.
type google struct {
	token, drive http.HandlerFunc
	refreshes    atomic.Int32
}

func (g *google) serve(t *testing.T) *Sensor {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		g.refreshes.Add(1)
		g.token(w, r)
	})
	mux.HandleFunc("/drive/v3/about", g.drive)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return newSensor(credentials, server.URL+"/token", server.URL+"/drive/v3/about", func() time.Time { return collected })
}

func issued(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"access_token":"`+accessToken+`","token_type":"Bearer","expires_in":3599}`)
}

func answering(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

func refused(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// assertNoCredential fails when an error carries any credential, raw or as the Basic
// authorization header would encode it.
func assertNoCredential(t *testing.T, err error) {
	t.Helper()
	basic := base64.StdEncoding.EncodeToString([]byte(credentials.ClientID + ":" + credentials.ClientSecret))
	for _, secret := range []string{credentials.ClientSecret, credentials.RefreshToken, accessToken, basic} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error = %q, want it to keep every credential out", err)
		}
	}
}

// spec: services.md#google-drive — the free space, and its share of the limit.
func TestCollectReadsTheQuota(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		free, pct float64
	}{
		{"a quarter used", `{"storageQuota":{"limit":"107374182400","usage":"26843545600"}}`, 80530636800, 75},
		{"a third used", `{"storageQuota":{"limit":"16106127360","usage":"5368709120"}}`, 10737418240, 66.67},
		{"a usage equal to the limit", `{"storageQuota":{"limit":"16106127360","usage":"16106127360"}}`, 0, 0},
		{"a usage above the limit", `{"storageQuota":{"limit":"16106127360","usage":"17179869184"}}`, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := &google{token: issued, drive: answering(tc.body)}

			got, err := g.serve(t).Collect(context.Background())
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}

			want := []sensor.Measurement{
				{Metric: "gdrive.free_bytes", Value: tc.free, TS: collected},
				{Metric: "gdrive.free_pct", Value: tc.pct, TS: collected},
			}
			if len(got) != len(want) {
				t.Fatalf("collected %+v, want %+v", got, want)
			}
			for i := range want {
				if got[i].Metric != want[i].Metric || got[i].Value != want[i].Value || !got[i].TS.Equal(collected) || len(got[i].Labels) != 0 {
					t.Errorf("collected %+v, want %+v", got[i], want[i])
				}
			}
		})
	}
}

// spec: services.md#google-drive — the request asks for the quota alone, with the access
// token the refresh issued.
func TestCollectAsksForTheQuotaWithTheIssuedToken(t *testing.T) {
	var asked *http.Request
	g := &google{token: issued, drive: func(w http.ResponseWriter, r *http.Request) {
		asked = r
		answering(`{"storageQuota":{"limit":"100","usage":"50"}}`)(w, r)
	}}

	if _, err := g.serve(t).Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if asked.Method != http.MethodGet || asked.URL.Query().Get("fields") != "storageQuota" {
		t.Errorf("request = %s %s, want GET with fields=storageQuota", asked.Method, asked.URL)
	}
	if got := asked.Header.Get("Authorization"); got != "Bearer "+accessToken {
		t.Errorf("Authorization = %q, want the issued access token", got)
	}
}

// spec: services.md#google-drive — an account with unlimited storage has nothing to run out.
func TestCollectReportsNothingWithoutALimit(t *testing.T) {
	g := &google{token: issued, drive: answering(`{"storageQuota":{"usage":"26843545600"}}`)}

	got, err := g.serve(t).Collect(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("Collect = %+v, %v; want nothing and no error", got, err)
	}
}

// spec: services.md#google-drive — a figure that is not a count of bytes is an error.
func TestCollectRefusesAMalformedQuota(t *testing.T) {
	tests := map[string]string{
		"a limit of 0":            `{"storageQuota":{"limit":"0","usage":"0"}}`,
		"no usage":                `{"storageQuota":{"limit":"100"}}`,
		"no storageQuota":         `{}`,
		"a JSON number":           `{"storageQuota":{"limit":100,"usage":"50"}}`,
		"a negative figure":       `{"storageQuota":{"limit":"100","usage":"-1"}}`,
		"beyond a 64-bit integer": `{"storageQuota":{"limit":"99999999999999999999","usage":"1"}}`,
		"not digits":              `{"storageQuota":{"limit":"12a","usage":"1"}}`,
		"a body that is not JSON": `<html>`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			g := &google{token: issued, drive: answering(body)}

			got, err := g.serve(t).Collect(context.Background())
			if err == nil || len(got) != 0 {
				t.Fatalf("Collect = %+v, %v; want nothing and an error", got, err)
			}
		})
	}
}

// spec: services.md#google-drive — a refusal names its status and Google's reason.
func TestCollectNamesTheStatusAndTheReasonOfARefusal(t *testing.T) {
	g := &google{token: issued, drive: refused(http.StatusForbidden,
		`{"error":{"code":403,"message":"Drive API has not been used","errors":[{"reason":"accessNotConfigured"}]}}`)}

	_, err := g.serve(t).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "accessNotConfigured") {
		t.Fatalf("error = %v, want it to name 403 and accessNotConfigured", err)
	}
	assertNoCredential(t, err)
}

// spec: services.md#google-drive — Google out of reach is an error naming the host.
func TestCollectNamesTheHostItCannotReach(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	s := newSensor(credentials, server.URL+"/token", server.URL+"/drive/v3/about", time.Now)

	_, err := s.Collect(context.Background())
	host, _ := url.Parse(server.URL)
	if err == nil || !strings.Contains(err.Error(), host.Host) {
		t.Fatalf("error = %v, want it to name %s", err, host.Host)
	}
	assertNoCredential(t, err)
}

// spec: services.md#invariants — a call Google has not answered by the deadline is cancelled.
// spec: services.md#google-drive — and the error names the host that did not answer.
func TestCollectCancelsACallPastItsDeadline(t *testing.T) {
	cancelled := make(chan struct{})
	g := &google{token: issued, drive: func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(cancelled)
	}}
	s := g.serve(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := s.Collect(ctx)
	if err == nil {
		t.Fatal("Collect answered past its deadline without an error")
	}
	if !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("error = %q, want it to name the host that did not answer", err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the call to Google kept running past the deadline")
	}
}

// spec: services.md#authorization — each refusal of the token endpoint says what to fix,
// and no credential in any form.
func TestCollectSaysWhichCredentialTheTokenEndpointRefused(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"invalid_grant", "refresh token was refused"},
		{"invalid_client", "id or secret was refused"},
		{"unauthorized_client", "another client"},
		{"invalid_request", "invalid_request"},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			g := &google{
				token: refused(http.StatusBadRequest, `{"error":"`+tc.code+`","error_description":"Bad Request"}`),
				drive: answering(`{"storageQuota":{"limit":"100","usage":"50"}}`),
			}

			got, err := g.serve(t).Collect(context.Background())
			if err == nil || len(got) != 0 {
				t.Fatalf("Collect = %+v, %v; want nothing and an error", got, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to name %q", err, tc.want)
			}
			assertNoCredential(t, err)
		})
	}
}

// spec: services.md#authorization — a refused refresh is asked once, with the client's
// credentials in the form, not a second time another way.
func TestCollectAsksTheTokenEndpointOnce(t *testing.T) {
	var form url.Values
	g := &google{
		token: func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse the token request: %v", err)
			}
			form = r.PostForm
			refused(http.StatusBadRequest, `{"error":"invalid_grant"}`)(w, r)
		},
		drive: answering(`{}`),
	}

	if _, err := g.serve(t).Collect(context.Background()); err == nil {
		t.Fatal("Collect succeeded on a refused refresh")
	}

	if n := g.refreshes.Load(); n != 1 {
		t.Errorf("the token endpoint was asked %d times, want once", n)
	}
	if form.Get("client_id") != credentials.ClientID || form.Get("refresh_token") != credentials.RefreshToken ||
		form.Get("grant_type") != "refresh_token" {
		t.Errorf("token request form = %v, want the client and the refresh token", form)
	}
}
