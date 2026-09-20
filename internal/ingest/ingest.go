// Package ingest implements /api/v1/ingest: the only channel between an agent and the
// hub (docs/specs/ingest.md).
package ingest

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/api"
	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/storage"
)

const (
	maxBodyBytes      = 1 << 20 // 1 MiB
	requestsPerMinute = 60
)

// Handler validates shape, not meaning: whether a value crosses a threshold is the
// evaluation engine's business.
type Handler struct {
	config  *config.Config
	store   storage.Storage
	now     func() time.Time
	limiter *limiter
}

// NewHandler builds the endpoint; now is the hub clock that sets last-seen.
func NewHandler(cfg *config.Config, store storage.Storage, now func() time.Time) *Handler {
	return &Handler{config: cfg, store: store, now: now, limiter: newLimiter(requestsPerMinute)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	node, ok := authenticate(h.config, r)
	if !ok {
		fail(w, http.StatusUnauthorized, "unknown or missing token")
		return
	}
	received := h.now()
	if !h.limiter.allow(node.Name, received) {
		fail(w, http.StatusTooManyRequests, fmt.Sprintf("more than %d requests a minute", requestsPerMinute))
		return
	}

	req, status, err := decode(w, r)
	if err != nil {
		fail(w, status, err.Error())
		return
	}
	in, err := validate(req, received)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Node != node.Name {
		fail(w, http.StatusForbidden, "the node does not belong to this token")
		return
	}
	if err := h.store.SaveIngest(r.Context(), in); err != nil {
		// The agent only learns that the hub answered 500; the reason stays here.
		slog.Error("store ingest", "node", in.Node, "error", err)
		fail(w, http.StatusInternalServerError, "the measurements could not be stored")
		return
	}

	body := api.Response{}
	if req.ConfigVersion != node.Version {
		body = api.Response{ConfigVersion: node.Version, Config: deliver(node.Agent)}
		// A rollout is invisible otherwise: the version is opaque to the agent, so the
		// journal is the only place the two can be compared.
		slog.Info("deliver a configuration", "node", node.Name, "from", req.ConfigVersion, "to", node.Version)
	}
	write(w, http.StatusOK, body)
}

// authenticate resolves the bearer token to its node, comparing in constant time so that
// a wrong token cannot be found one character at a time.
func authenticate(cfg *config.Config, r *http.Request) (config.Node, bool) {
	header := r.Header.Get("Authorization")
	presented, found := strings.CutPrefix(header, "Bearer ")
	if !found || presented == "" {
		return config.Node{}, false
	}
	for _, node := range cfg.Nodes() {
		if subtle.ConstantTimeCompare([]byte(presented), []byte(node.Token)) == 1 {
			return node, true
		}
	}
	return config.Node{}, false
}

func decode(w http.ResponseWriter, r *http.Request) (api.Request, int, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var req api.Request
	body := json.NewDecoder(r.Body)
	if err := body.Decode(&req); err != nil {
		return api.Request{}, statusFor(err), fmt.Errorf("body is not valid JSON: %w", err)
	}
	// Decode stops after the first value, so anything behind it would be accepted in
	// silence: the body is one document or it is not one. Reading to the end is also what
	// finds a body over the limit whose first document is small.
	var trailing json.RawMessage
	if err := body.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return api.Request{}, http.StatusBadRequest, errors.New("body carries more than one JSON document")
		}
		return api.Request{}, statusFor(err), fmt.Errorf("body is not a single JSON document: %w", err)
	}
	return req, http.StatusOK, nil
}

// statusFor tells a body too large from a body that is merely malformed; reading past the
// limit is how the first one is discovered.
func statusFor(err error) int {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// validate turns a request into what storage takes, or refuses the whole batch: one bad
// measurement rejects every measurement beside it.
func validate(req api.Request, received time.Time) (storage.Ingest, error) {
	if req.Node == "" {
		return storage.Ingest{}, errors.New("node is required")
	}
	if req.Measurements == nil {
		return storage.Ingest{}, errors.New("measurements is required, and may be empty")
	}
	sent, err := timestamp("ts", req.TS)
	if err != nil {
		return storage.Ingest{}, err
	}

	in := storage.Ingest{
		Node:          req.Node,
		AgentVersion:  req.AgentVersion,
		ConfigVersion: req.ConfigVersion,
		ReceivedAt:    received,
		Manifest:      make([]storage.SensorStatus, 0, len(req.Manifest)),
		Measurements:  make([]storage.Measurement, 0, len(*req.Measurements)),
	}
	for _, status := range req.Manifest {
		in.Manifest = append(in.Manifest, storage.SensorStatus{Sensor: status.Sensor, Applicable: status.Applicable})
	}
	for _, m := range *req.Measurements {
		stored, err := validateMeasurement(m, sent)
		if err != nil {
			return storage.Ingest{}, err
		}
		in.Measurements = append(in.Measurements, stored)
	}
	return in, nil
}

func validateMeasurement(m api.Measurement, sent time.Time) (storage.Measurement, error) {
	if !api.MetricID.MatchString(m.Metric) {
		return storage.Measurement{}, fmt.Errorf("metric %q is not an id of [a-z0-9_.]", m.Metric)
	}
	if m.Value == nil || math.IsNaN(*m.Value) || math.IsInf(*m.Value, 0) {
		return storage.Measurement{}, fmt.Errorf("metric %s: value is required and must be finite", m.Metric)
	}

	collected := sent
	if m.TS != "" {
		var err error
		if collected, err = timestamp("metric "+m.Metric+": ts", m.TS); err != nil {
			return storage.Measurement{}, err
		}
	}
	return storage.Measurement{Metric: m.Metric, Labels: m.Labels, Value: *m.Value, TS: collected}, nil
}

func timestamp(key, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("%s is required", key)
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %q is not an RFC 3339 time", key, value)
	}
	return parsed, nil
}

func fail(w http.ResponseWriter, status int, message string) {
	write(w, status, api.ErrorBody{Error: message})
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status line is already sent, so a failed write can only be logged, not fixed.
	_ = json.NewEncoder(w).Encode(body)
}

// deliver flattens a resolved configuration onto the wire. It lives here rather than in
// the contract package: translating the hub's configuration is the hub's work, and the
// agent should not link the hub's YAML machinery to read a response.
func deliver(agent config.Agent) *api.AgentConfig {
	out := &api.AgentConfig{
		BaseTick:    api.FormatDuration(agent.BaseTick),
		Filesystems: agent.Filesystems,
		SkipMounts:  agent.SkipMounts,
		Sensors:     make(map[string]api.SensorConfig, len(agent.Sensors)),
	}
	for name, sensor := range agent.Sensors {
		out.Sensors[name] = api.SensorConfig{
			Enabled:  sensor.Enabled,
			Interval: api.FormatDuration(sensor.Interval),
		}
	}
	return out
}
