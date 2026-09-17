package config_test

import (
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/config"
)

// spec: hub-config.md#resolution — the agents' target layers most-specific-last, and no
// layer naming one leaves the node without a target.
func TestResolveLayersAgentTarget(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"no layer sets it", minimal, ""},
		{"the top level sets it", minimal + "\nagent_target: latest\n", "latest"},
		{"the class wins over the top level", minimal +
			"\nagent_target: latest\nclasses:\n  laptop:\n    agent_target: 1.4.0\n", "1.4.0"},
		{"the node wins over the class", `
agent_target: latest
classes:
  laptop:
    agent_target: 1.4.0
nodes:
  laptop-a:
    class: laptop
    token_env: MONITOR_TOKEN_LAPTOP_A
    agent_target: 1.5.0
`, "1.5.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := node(t, load(t, tc.body), "laptop-a").AgentTarget; got != tc.want {
				t.Errorf("AgentTarget = %q, want %q", got, tc.want)
			}
		})
	}
}

// spec: hub-config.md#startup — a target that is neither latest nor one version is refused
// wherever it is written, naming where.
func TestLoadRejectsAnAgentTargetThatIsNotOne(t *testing.T) {
	values := []string{"1.4", "v1.4.0", "01.4.0", "1.4.0.1", "1.1234567890.0", `""`, "", "Latest", "1.4.0-rc1"}
	places := []struct {
		name  string
		body  func(value string) string
		names string
	}{
		{"the top level", func(v string) string { return minimal + "\nagent_target: " + v + "\n" }, "agent_target"},
		{"a class", func(v string) string {
			return minimal + "\nclasses:\n  laptop:\n    agent_target: " + v + "\n"
		}, "class laptop"},
		{"a node", func(v string) string { return minimal + "    agent_target: " + v + "\n" }, "node laptop-a"},
	}
	for _, place := range places {
		for _, value := range values {
			t.Run(place.name+" "+value, func(t *testing.T) {
				t.Setenv(tokenEnv, token)
				_, err := config.Load(write(t, place.body(value)))
				if err == nil {
					t.Fatalf("Load accepted agent_target %q in %s", value, place.name)
				}
				for _, want := range []string{"agent_target", place.names} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error = %q, want it to name %q", err, want)
					}
				}
			})
		}
	}
}

// spec: hub-config.md#configuration-version — the target never reaches the ingest response,
// so it cannot make an agent fetch its configuration again.
func TestVersionIgnoresTheAgentTarget(t *testing.T) {
	before := node(t, load(t, minimal), "laptop-a").Version
	after := node(t, load(t, minimal+"\nagent_target: 1.4.0\n"), "laptop-a").Version
	if before != after {
		t.Errorf("version changed from %s to %s with the agent target alone", before, after)
	}
}

// spec: hub-config.md#startup — a token is what tells the hub which node asks, so two
// variables holding the same one are refused, naming both nodes and not the value.
func TestLoadRejectsTheSameTokenInTwoVariables(t *testing.T) {
	t.Setenv(tokenEnv, token)
	t.Setenv("MONITOR_TOKEN_SERVER_B", token)
	_, err := config.Load(write(t, minimal+`  server-b:
    class: server
    token_env: MONITOR_TOKEN_SERVER_B
`))
	if err == nil {
		t.Fatal("Load accepted two nodes holding the same token")
	}
	for _, want := range []string{"laptop-a", "server-b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %s", err, want)
		}
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("error = %q names the token", err)
	}
}
