package command

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// fakeDockerScript builds a tiny shell script we hand back through
// exec.Command so docker invocations during tests are recorded to a log
// file instead of actually running docker. Stdout is whatever we tell
// the fake to print; exit code is 0 unless overridden.
func installFakeDocker(t *testing.T, behavior map[string]fakeDockerCall) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = dir + "/docker.log"

	prev := dockerCmdFn
	dockerCmdFn = func(name string, args ...string) *exec.Cmd {
		if name != "docker" {
			t.Fatalf("expected docker, got %s", name)
		}
		key := strings.Join(args, " ")
		stdout, exitCode := "", 0
		if call, ok := behavior[key]; ok {
			stdout = call.stdout
			exitCode = call.exit
		} else if call, ok := behavior["*"]; ok {
			stdout = call.stdout
			exitCode = call.exit
		}
		// Use sh to record the call and emit configured output.
		script := fmt.Sprintf(
			"echo %q >> %s\nprintf %q\nexit %d\n",
			"docker "+key, logPath, stdout, exitCode,
		)
		return exec.Command("sh", "-c", script)
	}
	t.Cleanup(func() { dockerCmdFn = prev })
	return logPath
}

type fakeDockerCall struct {
	stdout string
	exit   int
}

func readLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func TestDockerStartPostgresCreatesContainerWhenMissing(t *testing.T) {
	logPath := installFakeDocker(t, map[string]fakeDockerCall{
		"ps -a --filter name=^/theraprac-agent-z-postgres$ --format {{.State}}": {stdout: "", exit: 0},
		"*": {stdout: "", exit: 0},
	})

	if err := dockerStartPostgres("agent-z", 5732); err != nil {
		t.Fatalf("dockerStartPostgres: %v", err)
	}

	calls := readLog(t, logPath)
	var sawRun, sawReady bool
	for _, c := range calls {
		if strings.Contains(c, "run -d --name theraprac-agent-z-postgres") &&
			strings.Contains(c, "5732:5432") &&
			strings.Contains(c, "POSTGRES_DB=theraprac_dev") &&
			strings.Contains(c, "theraprac-agent-z-postgres-data:/var/lib/postgresql/data") &&
			strings.Contains(c, "postgres:17") {
			sawRun = true
		}
		if strings.Contains(c, "exec theraprac-agent-z-postgres pg_isready") {
			sawReady = true
		}
	}
	if !sawRun {
		t.Errorf("docker run not invoked with expected args. calls=%v", calls)
	}
	if !sawReady {
		t.Errorf("pg_isready not polled. calls=%v", calls)
	}
}

func TestDockerStartPostgresNoOpWhenRunning(t *testing.T) {
	logPath := installFakeDocker(t, map[string]fakeDockerCall{
		"ps -a --filter name=^/theraprac-agent-z-postgres$ --format {{.State}}": {stdout: "running", exit: 0},
		"*": {stdout: "", exit: 0},
	})
	if err := dockerStartPostgres("agent-z", 5732); err != nil {
		t.Fatalf("dockerStartPostgres: %v", err)
	}
	for _, c := range readLog(t, logPath) {
		if strings.Contains(c, "run -d") || strings.Contains(c, " start ") {
			t.Errorf("running container should not trigger run/start: %s", c)
		}
	}
}

func TestDockerStartPostgresReusesStoppedContainer(t *testing.T) {
	logPath := installFakeDocker(t, map[string]fakeDockerCall{
		"ps -a --filter name=^/theraprac-agent-z-postgres$ --format {{.State}}": {stdout: "exited", exit: 0},
		"*": {stdout: "", exit: 0},
	})
	if err := dockerStartPostgres("agent-z", 5732); err != nil {
		t.Fatalf("dockerStartPostgres: %v", err)
	}
	var sawStart, sawRun bool
	for _, c := range readLog(t, logPath) {
		if c == "docker start theraprac-agent-z-postgres" {
			sawStart = true
		}
		if strings.Contains(c, "run -d") {
			sawRun = true
		}
	}
	if !sawStart {
		t.Errorf("expected docker start for stopped container")
	}
	if sawRun {
		t.Errorf("should not docker run when container exists")
	}
}

func TestDockerStartMailpitCreatesContainerWithBothPorts(t *testing.T) {
	logPath := installFakeDocker(t, map[string]fakeDockerCall{
		"ps -a --filter name=^/theraprac-agent-z-mailpit$ --format {{.State}}": {stdout: "", exit: 0},
		"*": {stdout: "", exit: 0},
	})
	if err := dockerStartMailpit("agent-z", 1225, 8225); err != nil {
		t.Fatalf("dockerStartMailpit: %v", err)
	}
	var sawRun bool
	for _, c := range readLog(t, logPath) {
		if strings.Contains(c, "run -d --name theraprac-agent-z-mailpit") &&
			strings.Contains(c, "1225:1025") &&
			strings.Contains(c, "8225:8025") &&
			strings.Contains(c, "axllent/mailpit") {
			sawRun = true
		}
	}
	if !sawRun {
		t.Errorf("mailpit run not invoked with expected args")
	}
}

func TestDockerRemoveAgentRemovesContainersAndVolume(t *testing.T) {
	logPath := installFakeDocker(t, map[string]fakeDockerCall{
		"ps -a --filter name=^/theraprac-agent-z-postgres$ --format {{.State}}": {stdout: "running", exit: 0},
		"ps -a --filter name=^/theraprac-agent-z-mailpit$ --format {{.State}}":  {stdout: "exited", exit: 0},
		"*": {stdout: "", exit: 0},
	})
	if err := dockerRemoveAgent("agent-z"); err != nil {
		t.Fatalf("dockerRemoveAgent: %v", err)
	}
	calls := readLog(t, logPath)
	var sawPgRm, sawMailRm, sawVolRm bool
	for _, c := range calls {
		if c == "docker rm -fv theraprac-agent-z-postgres" {
			sawPgRm = true
		}
		if c == "docker rm -fv theraprac-agent-z-mailpit" {
			sawMailRm = true
		}
		if c == "docker volume rm theraprac-agent-z-postgres-data" {
			sawVolRm = true
		}
	}
	if !sawPgRm || !sawMailRm || !sawVolRm {
		t.Errorf("expected all three remove calls, got %v", calls)
	}
}

func TestDockerRemoveAgentSkipsMissingContainers(t *testing.T) {
	logPath := installFakeDocker(t, map[string]fakeDockerCall{
		"ps -a --filter name=^/theraprac-agent-z-postgres$ --format {{.State}}": {stdout: "", exit: 0},
		"ps -a --filter name=^/theraprac-agent-z-mailpit$ --format {{.State}}":  {stdout: "", exit: 0},
		"volume rm theraprac-agent-z-postgres-data": {stdout: "Error: No such volume: theraprac-agent-z-postgres-data\n", exit: 1},
		"*": {stdout: "", exit: 0},
	})
	if err := dockerRemoveAgent("agent-z"); err != nil {
		t.Fatalf("dockerRemoveAgent: %v (missing resources should be tolerated)", err)
	}
	for _, c := range readLog(t, logPath) {
		if strings.Contains(c, "rm -fv") {
			t.Errorf("should not call rm on missing containers, got %s", c)
		}
	}
}

func TestMailpitSMTPPortDerivedFromUIPort(t *testing.T) {
	for _, tc := range []struct {
		ui   int
		want int
	}{
		{8125, 1125}, // agent-a
		{8225, 1225}, // agent-b
		{8325, 1325}, // agent-c
	} {
		got := mailpitSMTPPort(tc.ui)
		if got != tc.want {
			t.Errorf("mailpitSMTPPort(%d) = %d, want %d", tc.ui, got, tc.want)
		}
	}
}

func TestApplyEnvOverridesReplacesExistingKeys(t *testing.T) {
	body := strings.Join([]string{
		"# top comment",
		"DB_HOST=localhost",
		"DB_PORT=5432",
		"DB_NAME=theraprac",
		"",
		"SERVER_PORT=8080",
	}, "\n")
	out, err := applyEnvOverrides(body, map[string]string{
		"DB_PORT":     "5532",
		"SERVER_PORT": "8180",
	})
	if err != nil {
		t.Fatalf("applyEnvOverrides: %v", err)
	}
	if !strings.Contains(out, "DB_PORT=5532") {
		t.Errorf("DB_PORT not overridden: %s", out)
	}
	if !strings.Contains(out, "SERVER_PORT=8180") {
		t.Errorf("SERVER_PORT not overridden: %s", out)
	}
	if strings.Contains(out, "DB_PORT=5432") || strings.Contains(out, "SERVER_PORT=8080") {
		t.Errorf("old values should not survive: %s", out)
	}
	if !strings.Contains(out, "# top comment") || !strings.Contains(out, "DB_HOST=localhost") {
		t.Errorf("comments / unrelated keys not preserved: %s", out)
	}
}

func TestApplyEnvOverridesAppendsMissingKeys(t *testing.T) {
	body := "EXISTING=1\n"
	out, err := applyEnvOverrides(body, map[string]string{
		"DB_PORT":     "5532",
		"SERVER_PORT": "8180",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "# st agent workspace — per-agent overrides") {
		t.Errorf("missing-key block header absent: %s", out)
	}
	if !strings.Contains(out, "DB_PORT=5532") || !strings.Contains(out, "SERVER_PORT=8180") {
		t.Errorf("missing keys not appended: %s", out)
	}
	if !strings.Contains(out, "EXISTING=1") {
		t.Errorf("existing key dropped: %s", out)
	}
}

func TestPerRepoEnvOverridesAPI(t *testing.T) {
	got := perRepoEnvOverrides("theraprac-api", agentWorkspacePorts{API: 8180, Web: 3100, DB: 5532, Mailpit: 8125})
	if got["SERVER_PORT"] != "8180" || got["DB_PORT"] != "5532" || got["DB_HOST"] != "localhost" {
		t.Errorf("api overrides wrong: %+v", got)
	}
}

func TestPerRepoEnvOverridesWeb(t *testing.T) {
	got := perRepoEnvOverrides("theraprac-web", agentWorkspacePorts{API: 8180, Web: 3100, DB: 5532, Mailpit: 8125})
	if got["PORT"] != "3100" || got["API_BASE_URL"] != "http://localhost:8180" {
		t.Errorf("web overrides wrong: %+v", got)
	}
}

func TestPerRepoEnvOverridesUnknownRepoIsNoOp(t *testing.T) {
	got := perRepoEnvOverrides("theraprac-infra", agentWorkspacePorts{API: 8180})
	if len(got) != 0 {
		t.Errorf("infra/workspace should have no overrides: %+v", got)
	}
}

func TestMaterializeEnvCopiesAndOverlaysAPIEnv(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(source+"/.env", []byte("DB_HOST=localhost\nDB_PORT=5432\nDB_NAME=theraprac\nSERVER_PORT=8080\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ports := agentWorkspacePorts{API: 8180, DB: 5532}
	if err := materializeEnv("theraprac-api", source, target, ports); err != nil {
		t.Fatalf("materializeEnv: %v", err)
	}
	body, err := os.ReadFile(target + "/.env")
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, "DB_PORT=5532") || !strings.Contains(got, "SERVER_PORT=8180") {
		t.Errorf("agent .env not overlaid: %s", got)
	}
	if !strings.Contains(got, "DB_NAME=theraprac") {
		t.Errorf("untouched key dropped: %s", got)
	}
	srcAfter, _ := os.ReadFile(source + "/.env")
	if strings.Contains(string(srcAfter), "5532") {
		t.Errorf("source .env was mutated; isolation broken: %s", string(srcAfter))
	}
}
