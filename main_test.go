package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSidecarHelperProcess(t *testing.T) {
	if os.Getenv("GITCHAT_TEST_SIDECAR") != "1" {
		return
	}
	if err := run(); err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

type testSidecar struct {
	command *exec.Cmd
	input   io.WriteCloser
	done    chan struct{}
	err     error
	output  bytes.Buffer
}

func startTestSidecar(t *testing.T, directory, ready string) *testSidecar {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestSidecarHelperProcess$")
	command.Dir = directory
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		if strings.HasPrefix(name, "GITCHAT_") || strings.HasPrefix(name, "OPENAI_") || name == "DATA_DIR" || name == "PORT" || name == "GIN_MODE" {
			continue
		}
		command.Env = append(command.Env, variable)
	}
	command.Env = append(command.Env, "GITCHAT_TEST_SIDECAR=1", "GITCHAT_MODE=demo", "GIN_MODE=release", "GITCHAT_ADDR=127.0.0.1:0", "GITCHAT_READY_FILE="+ready, "GITCHAT_EXIT_ON_STDIN_CLOSE=1", "DATA_DIR="+filepath.Join(directory, "数据 space"))
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	child := &testSidecar{command: command, input: input, done: make(chan struct{})}
	command.Stdout = &child.output
	command.Stderr = &child.output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { child.err = command.Wait(); close(child.done) }()
	t.Cleanup(func() {
		_ = child.input.Close()
		select {
		case <-child.done:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-child.done
		}
	})
	return child
}

func readSidecarURL(t *testing.T, child *testSidecar, path string) string {
	t.Helper()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-child.done:
			t.Fatalf("sidecar exited before readiness: %v %s", child.err, child.output.String())
		case <-timeout.C:
			t.Fatal("sidecar did not publish readiness")
		case <-tick.C:
			data, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			var ready struct {
				URL string `json:"url"`
				PID int    `json:"pid"`
			}
			if err := json.Unmarshal(data, &ready); err != nil {
				t.Fatalf("published partial readiness JSON: %v", err)
			}
			if ready.PID != child.command.Process.Pid || !strings.HasPrefix(ready.URL, "http://127.0.0.1:") || strings.HasSuffix(ready.URL, ":0") {
				t.Fatalf("incorrect readiness: %s", data)
			}
			return ready.URL
		}
	}
}

func stopTestSidecar(t *testing.T, child *testSidecar, path string) {
	t.Helper()
	if err := child.input.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-child.done:
		if child.err != nil {
			t.Fatalf("sidecar did not shut down cleanly: %v %s", child.err, child.output.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stdin EOF left sidecar running")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("clean shutdown left stale readiness file")
	}
}

func TestDesktopReadinessLifecycleAndPersistence(t *testing.T) {
	directory := t.TempDir()
	readyFile := filepath.Join(directory, "ready.json")
	child := startTestSidecar(t, directory, readyFile)
	base := readSidecarURL(t, child, readyFile)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(readyFile)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("readiness permissions are not private: %v", err)
		}
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 3 * time.Second}
	response, err := client.Post(base+"/api/sessions", "application/json", strings.NewReader(`{"title":"跨平台持久化"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("ready server rejected session: %d", response.StatusCode)
	}
	// A second process cannot use the same data directory or publish readiness.
	otherReady := filepath.Join(directory, "second.json")
	other := startTestSidecar(t, directory, otherReady)
	select {
	case <-other.done:
		if other.err == nil || !strings.Contains(other.output.String(), "data directory is in use") {
			t.Fatalf("exclusive directory lock failed: %v %s", other.err, other.output.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("second sidecar did not reject a locked directory")
	}
	if _, err := os.Stat(otherReady); !os.IsNotExist(err) {
		t.Fatal("failed sidecar published readiness")
	}
	stopTestSidecar(t, child, readyFile)
	restarted := startTestSidecar(t, directory, readyFile)
	base = readSidecarURL(t, restarted, readyFile)
	response, err = client.Get(base + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Contains(body, []byte("跨平台持久化")) {
		t.Fatalf("sidecar restart lost workspace: %d %s", response.StatusCode, body)
	}
	stopTestSidecar(t, restarted, readyFile)
}

func TestReadinessRejectsUnsafePathsAndPublicBind(t *testing.T) {
	loopback := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	if err := writeReadyFile("relative.json", loopback); err == nil {
		t.Fatal("relative readiness path accepted")
	}
	directory := t.TempDir()
	if err := writeReadyFile(filepath.Join(directory, "public.json"), &net.TCPAddr{IP: net.IPv4zero, Port: 12345}); err == nil {
		t.Fatal("public sidecar readiness accepted")
	}
	if err := writeReadyFile(directory, loopback); err == nil {
		t.Fatal("readiness directory accepted as a file")
	}
}
