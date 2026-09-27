package config

import (
	"fmt"
	"time"
)

// ServiceClass is the compiled-in class of a node the hub collects itself: an online
// service rather than a machine (ADR 0039). The name is reserved: a class of that name in
// the file overrides this one key by key, and is never a machine's.
const ServiceClass = "service"

const gdriveSensor = "gdrive"

// serviceSensors are the sensors only the hub carries; every other sensor is an agent's.
var serviceSensors = map[string]bool{gdriveSensor: true}

// The Google Drive credentials are deployment secrets and live in the environment, never
// in the file (ADR 0007).
const (
	gdriveClientIDEnv     = "MONITOR_GDRIVE_CLIENT_ID"
	gdriveClientSecretEnv = "MONITOR_GDRIVE_CLIENT_SECRET"
	gdriveRefreshTokenEnv = "MONITOR_GDRIVE_REFRESH_TOKEN"
)

// GoogleDrive is what the gdrive sensor authorizes with. It is empty unless a service node
// runs gdrive.
type GoogleDrive struct {
	ClientID, ClientSecret, RefreshToken string
}

// String keeps the credentials out of any print of the configuration.
func (g GoogleDrive) String() string {
	return fmt.Sprintf("Google Drive credentials set: %t", g != GoogleDrive{})
}

// GoogleDrive is the credentials of the gdrive sensor.
func (c *Config) GoogleDrive() GoogleDrive { return c.gdrive }

// reserved is what a refusal says of the service class, which a file written before it
// existed may use for machines.
const reserved = "class " + ServiceClass + " is reserved for nodes the hub collects itself (ADR 0039); rename a machine class of that name"

// forHost keeps the top-level sensor layer to the sensors a class's host can run, so that
// switching a sensor on for every class never hands it to a host that cannot run it.
func forHost(service bool, layer map[string]fileSensor) map[string]fileSensor {
	out := make(map[string]fileSensor, len(layer))
	for name, settings := range layer {
		if serviceSensors[name] == service {
			out[name] = settings
		}
	}
	return out
}

// checkHost refuses a sensor enabled where its host cannot run it: the hub carries only
// its own sensors, and an agent carries none of them.
func checkHost(where string, service bool, sensors map[string]Sensor) error {
	for _, name := range sorted(sensors) {
		if !sensors[name].Enabled || serviceSensors[name] == service {
			continue
		}
		if service {
			return fmt.Errorf("%ssensor %s runs on an agent: %s", where, name, reserved)
		}
		return fmt.Errorf("%ssensor %s is collected by the hub, only on a node of class %s", where, name, ServiceClass)
	}
	return nil
}

// checkServiceNode refuses a service node that would fall silent with nothing wrong: one
// that stores nothing, and one whose window a single failed collection outlasts. Between
// two stored measurements lie two intervals, each collection due only on a tick after its
// interval, and a failed one may hold its tick for half a tick more.
func checkServiceNode(name string, node Node) error {
	var longest time.Duration
	for _, sensor := range node.Agent.Sensors {
		if sensor.Enabled {
			longest = max(longest, sensor.Interval)
		}
	}
	if longest == 0 {
		return fmt.Errorf("node %s: a %s node enabling no sensor never stores a measurement, so it would fall silent for good", name, ServiceClass)
	}
	if bound := 2*longest + 3*node.Agent.BaseTick; node.SilenceAfter < bound {
		return fmt.Errorf("node %s: silence_after %v is shorter than %v, two of its longest interval and three base ticks, so one failed collection could make it fall silent",
			name, node.SilenceAfter, bound)
	}
	return nil
}

// resolveGoogleDrive reads the credentials only when some service node runs gdrive.
func resolveGoogleDrive(nodes map[string]Node) (GoogleDrive, error) {
	needed := false
	for _, node := range nodes {
		needed = needed || (node.Service() && node.Agent.Sensors[gdriveSensor].Enabled)
	}
	if !needed {
		return GoogleDrive{}, nil
	}
	because := "a " + ServiceClass + " node runs " + gdriveSensor + ", and it has no default"
	var out GoogleDrive
	for _, field := range []struct {
		env  string
		into *string
	}{
		{gdriveClientIDEnv, &out.ClientID},
		{gdriveClientSecretEnv, &out.ClientSecret},
		{gdriveRefreshTokenEnv, &out.RefreshToken},
	} {
		value, err := envValue(field.env, because)
		if err != nil {
			return GoogleDrive{}, err
		}
		*field.into = value
	}
	return out, nil
}
