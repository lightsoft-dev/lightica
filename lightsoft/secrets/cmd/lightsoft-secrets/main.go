// Command lightsoft-secrets lets a Multica agent read the team secrets it has
// been granted. It runs inside an agent task: the daemon injects
// LIGHTSOFT_SECRETS_TOKEN / LIGHTSOFT_SECRETS_URL (from the agent's
// custom_env, set when the agent is enrolled in the team client) and
// MULTICA_TOKEN / MULTICA_TASK_ID (for every task).
//
//	lightsoft-secrets list
//	lightsoft-secrets get NAME
//	lightsoft-secrets exec [--env TARGET=NAME]... [NAME...] -- COMMAND [ARGS...]
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const defaultURL = "http://127.0.0.1:13300/lightsoft-secrets"

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const usage = `usage:
  lightsoft-secrets list
      List the secrets this agent may read (names only).
  lightsoft-secrets get NAME
      Print one value to stdout. Prefer exec: printed values end up in logs.
  lightsoft-secrets exec [--env TARGET=NAME]... [NAME...] -- COMMAND [ARGS...]
      Run COMMAND with secrets in its environment, without printing them.
      NAME sets env var NAME; --env TARGET=NAME sets TARGET to secret NAME.

Every read is recorded in the team client (시크릿 > 에이전트 접근 기록).
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lightsoft-secrets:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(usage)
		return nil
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		return c.list()
	case "get":
		if len(args) != 2 {
			return errors.New("usage: lightsoft-secrets get NAME")
		}
		v, err := c.get(args[1])
		if err != nil {
			return err
		}
		_, err = os.Stdout.WriteString(v)
		return err
	case "exec":
		return c.exec(args[1:])
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}

type client struct {
	base      string
	token     string
	taskToken string
	taskID    string
	http      *http.Client
}

func newClient() (*client, error) {
	c := &client{
		base:      strings.TrimRight(firstNonEmpty(os.Getenv("LIGHTSOFT_SECRETS_URL"), defaultURL), "/"),
		token:     strings.TrimSpace(os.Getenv("LIGHTSOFT_SECRETS_TOKEN")),
		taskToken: strings.TrimSpace(os.Getenv("MULTICA_TOKEN")),
		taskID:    strings.TrimSpace(os.Getenv("MULTICA_TASK_ID")),
		http:      &http.Client{Timeout: 15 * time.Second},
	}
	if c.token == "" {
		return nil, errors.New("LIGHTSOFT_SECRETS_TOKEN is not set: enroll this agent in the team client (시크릿 > 에이전트 연결); it applies from the agent's next run")
	}
	if c.taskToken == "" {
		return nil, errors.New("MULTICA_TOKEN is not set: run this inside a Multica agent task")
	}
	return c, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (c *client) call(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Multica-Token", c.taskToken)
	if c.taskID != "" {
		req.Header.Set("X-Task-ID", c.taskID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s (HTTP %d)", e.Error, resp.StatusCode)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

func (c *client) list() error {
	var out struct {
		Secrets []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"secrets"`
	}
	if err := c.call("/v1/secrets", &out); err != nil {
		return err
	}
	if len(out.Secrets) == 0 {
		fmt.Println("(no secrets granted to this agent)")
		return nil
	}
	for _, s := range out.Secrets {
		if s.Description != "" {
			fmt.Printf("%s\t%s\n", s.Name, s.Description)
		} else {
			fmt.Println(s.Name)
		}
	}
	return nil
}

func (c *client) get(name string) (string, error) {
	var out struct {
		Value string `json:"value"`
	}
	if err := c.call("/v1/secrets/"+url.PathEscape(name), &out); err != nil {
		return "", err
	}
	return out.Value, nil
}

func (c *client) exec(args []string) error {
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep == len(args)-1 {
		return errors.New("usage: lightsoft-secrets exec [--env TARGET=NAME]... [NAME...] -- COMMAND [ARGS...]")
	}
	mapping := map[string]string{} // env var -> secret name
	opts := args[:sep]
	for i := 0; i < len(opts); i++ {
		a := opts[i]
		switch {
		case a == "--env":
			if i+1 >= len(opts) {
				return errors.New("--env needs TARGET=NAME")
			}
			i++
			if err := addMapping(mapping, opts[i]); err != nil {
				return err
			}
		case strings.HasPrefix(a, "--env="):
			if err := addMapping(mapping, strings.TrimPrefix(a, "--env=")); err != nil {
				return err
			}
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			if err := addMapping(mapping, a+"="+a); err != nil {
				return err
			}
		}
	}
	if len(mapping) == 0 {
		return errors.New("name at least one secret before --")
	}
	env := os.Environ()
	for target, name := range mapping {
		v, err := c.get(name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		env = append(env, target+"="+v)
	}
	cmd := args[sep+1:]
	path, err := exec.LookPath(cmd[0])
	if err != nil {
		return err
	}
	return syscall.Exec(path, cmd, env)
}

func addMapping(m map[string]string, spec string) error {
	target, name, ok := strings.Cut(spec, "=")
	if !ok || !envNamePattern.MatchString(target) || name == "" {
		return fmt.Errorf("invalid mapping %q (want TARGET=NAME)", spec)
	}
	m[target] = name
	return nil
}
