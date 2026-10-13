// Package launch starts a coding-agent CLI pre-wired to the local proxy.
package launch

import (
	"fmt"
	"net"
	"os"
	"os/exec"
)

// aliases maps friendly agent names to their executables. Only agents that speak
// the Anthropic Messages API can use the proxy; others are run by literal command.
var aliases = map[string]string{
	"claude": "claude", // Claude Code
}

// Resolve maps an agent alias to its executable name (identity if unknown).
func Resolve(agent string) string {
	if bin, ok := aliases[agent]; ok {
		return bin
	}
	return agent
}

// HTTPBaseURL turns a listen address into a reachable base URL for the agent.
func HTTPBaseURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// Env returns the environment for pointing an Anthropic-API agent at the proxy.
func Env(baseURL, token string) []string {
	if token == "" {
		token = "local" // the proxy holds the real provider keys; any value works on loopback
	}
	return append(os.Environ(),
		"ANTHROPIC_BASE_URL="+baseURL,
		"ANTHROPIC_AUTH_TOKEN="+token,
		"ANTHROPIC_API_KEY="+token,
	)
}

// Run execs the agent command with the proxy environment, inheriting stdio. It
// replaces the current process's std streams and blocks until the agent exits.
func Run(baseURL, token, command string, args []string) error {
	bin, err := exec.LookPath(command)
	if err != nil {
		return fmt.Errorf("agent %q not found in PATH: %w", command, err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = Env(baseURL, token)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
