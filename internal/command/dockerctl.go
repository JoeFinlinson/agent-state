package command

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// dockerCmdFn lets tests substitute exec.Command with a fake.
var dockerCmdFn = exec.Command

const (
	postgresImage       = "postgres:17"
	mailpitImage        = "axllent/mailpit:latest"
	pgInternalPort      = "5432"
	mailpitInternalSMTP = "1025"
	mailpitInternalUI   = "8025"
)

// pgBootstrapUser/Password/DB match the central theraprac docker-compose
// postgres so a per-agent container is a drop-in replacement: the api
// repo's existing .env (DB_ADMIN_USER=theraprac, DB_NAME=theraprac, etc.)
// works unmodified except for DB_PORT, and `make db-migrate` connects
// without further .env edits.
//
// App-level users (theraprac_app, theraprac_materializer, etc.) are
// created by liquibase migrations during db-migrate.
const (
	pgBootstrapUser     = "theraprac"
	pgBootstrapPassword = "theraprac_dev_password"
	pgBootstrapDB       = "theraprac"
)

func postgresContainerName(agentID string) string { return "theraprac-" + agentID + "-postgres" }
func mailpitContainerName(agentID string) string  { return "theraprac-" + agentID + "-mailpit" }
func postgresVolumeName(agentID string) string    { return "theraprac-" + agentID + "-postgres-data" }

// containerState returns "running", "exited", "created", "paused", "dead",
// or empty string when the container does not exist.
func containerState(name string) (string, error) {
	out, err := runDocker("ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.State}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func runDocker(args ...string) (string, error) {
	cmd := dockerCmdFn("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// dockerStartPostgres starts (or no-ops) the per-agent postgres container
// on the given host port. Idempotent: handles missing/stopped/running.
func dockerStartPostgres(agentID string, dbPort int) error {
	name := postgresContainerName(agentID)
	state, err := containerState(name)
	if err != nil {
		return err
	}
	switch state {
	case "running":
		return pgWaitReady(name, 30*time.Second)
	case "":
		args := []string{
			"run", "-d",
			"--name", name,
			"--restart", "unless-stopped",
			"-p", fmt.Sprintf("%d:%s", dbPort, pgInternalPort),
			"-e", "POSTGRES_USER=" + pgBootstrapUser,
			"-e", "POSTGRES_PASSWORD=" + pgBootstrapPassword,
			"-e", "POSTGRES_DB=" + pgBootstrapDB,
			"-e", "POSTGRES_INITDB_ARGS=--encoding=UTF-8 --lc-collate=C --lc-ctype=C",
			"-v", postgresVolumeName(agentID) + ":/var/lib/postgresql/data",
			postgresImage,
		}
		if _, err := runDocker(args...); err != nil {
			return err
		}
	default:
		if _, err := runDocker("start", name); err != nil {
			return err
		}
	}
	return pgWaitReady(name, 30*time.Second)
}

// dockerStartMailpit starts (or no-ops) the per-agent mailpit container
// on the given SMTP and UI host ports.
func dockerStartMailpit(agentID string, smtpPort, uiPort int) error {
	name := mailpitContainerName(agentID)
	state, err := containerState(name)
	if err != nil {
		return err
	}
	switch state {
	case "running":
		return nil
	case "":
		args := []string{
			"run", "-d",
			"--name", name,
			"--restart", "unless-stopped",
			"-p", fmt.Sprintf("%d:%s", smtpPort, mailpitInternalSMTP),
			"-p", fmt.Sprintf("%d:%s", uiPort, mailpitInternalUI),
			mailpitImage,
		}
		if _, err := runDocker(args...); err != nil {
			return err
		}
	default:
		if _, err := runDocker("start", name); err != nil {
			return err
		}
	}
	return nil
}

// pgWaitReady polls pg_isready until success or timeout.
func pgWaitReady(containerName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := dockerCmdFn("docker", "exec", containerName, "pg_isready", "-U", pgBootstrapUser)
		if err := cmd.Run(); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("postgres in container %s not ready within %s", containerName, timeout)
}

// dockerRemoveAgent stops and removes per-agent containers and volumes.
// Missing resources are not errors.
func dockerRemoveAgent(agentID string) error {
	var firstErr error
	for _, name := range []string{postgresContainerName(agentID), mailpitContainerName(agentID)} {
		state, _ := containerState(name)
		if state == "" {
			continue
		}
		if _, err := runDocker("rm", "-fv", name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if _, err := runDocker("volume", "rm", postgresVolumeName(agentID)); err != nil {
		if !strings.Contains(err.Error(), "No such volume") && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// mailpitSMTPPort derives the per-agent SMTP host port from the UI port.
// The algorithm keeps SMTP at 1025 + offset, where offset = mailpitUI - 8025.
func mailpitSMTPPort(mailpitUIPort int) int {
	return 1025 + (mailpitUIPort - 8025)
}
