package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/process"
	"hum/internal/protocol"
)

func TestOutputReadStripsTerminalControl(t *testing.T) {
	root := t.TempDir()
	var store *output.Store
	child := &daemonTestChild{pid: 6101, done: make(chan struct{})}
	supervisor, err := app.New(app.Options{StartProcess: func(spec process.Spec) (app.Child, error) {
		store = spec.Output
		return child, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := testServer(t, Config{Supervisor: supervisor})
	client, err := Dial(context.Background(), server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if _, err := client.Start(context.Background(), testStartRequest(root, "controls", testShell(t), "-c", "sleep 30")); err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("terminal-control child did not expose output store")
	}
	at := time.Unix(61, 7)
	stdoutRaw := "\x1b[31mready\x1b[0m\r\n"
	stderrRaw := "\x1b]0;warning\a\x1b[33mwarning\x1b[0m\n"
	systemRaw := "\x1b[35msystem\x1b[0m\n"
	controlOnlyRaw := "\x1b[2J\x1b[K"
	cursors := make([]output.Cursor, 4)
	for index, entry := range []struct {
		stream output.Stream
		text   string
	}{
		{output.Stdout, stdoutRaw}, {output.Stderr, stderrRaw}, {output.System, systemRaw}, {output.Stdout, controlOnlyRaw},
	} {
		cursors[index], err = store.Append(entry.stream, at.Add(time.Duration(index)*time.Second), entry.text)
		if err != nil {
			t.Fatal(err)
		}
	}

	response, err := client.Output(context.Background(), protocol.OutputRequest{
		Op: protocol.OpOutput, Name: "controls", Cwd: root, Stream: protocol.StreamBoth,
		MaxEntries: 16, MaxBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.version != protocol.Version {
		t.Fatalf("daemon protocol version = %d, want unchanged version %d", server.version, protocol.Version)
	}
	if response.Next == nil || *response.Next != cursors[3] || response.Oldest == nil || *response.Oldest != cursors[0] || response.Latest == nil || *response.Latest != cursors[3] {
		t.Fatalf("bounded metadata = %#v, want raw cursor boundaries", response)
	}
	if len(response.Entries) != 4 {
		t.Fatalf("bounded entries = %#v, want control-only entry retained", response.Entries)
	}
	wantText := []string{"ready\n", "warning\n", systemRaw, ""}
	wantStreams := []output.Stream{output.Stdout, output.Stderr, output.System, output.Stdout}
	for index, entry := range response.Entries {
		if entry.Cursor != cursors[index] || entry.Stream != wantStreams[index] || !entry.Time.Equal(at.Add(time.Duration(index)*time.Second)) || entry.Text != wantText[index] {
			t.Fatalf("bounded entry %d = %#v, want cursor=%d stream=%q time=%v text=%q", index, entry, cursors[index], wantStreams[index], at.Add(time.Duration(index)*time.Second), wantText[index])
		}
	}

	stored, err := store.Read(output.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Entries) != 4 || stored.Entries[0].Text != stdoutRaw || stored.Entries[1].Text != stderrRaw || stored.Entries[2].Text != systemRaw || stored.Entries[3].Text != controlOnlyRaw {
		t.Fatalf("stored entries = %#v, want raw text unchanged", stored.Entries)
	}

	heavyRaw := "\x1b[31mfit\x1b[0m"
	heavyCursor, err := store.Append(output.Stdout, at, heavyRaw)
	if err != nil {
		t.Fatal(err)
	}
	after := protocol.Cursor(cursors[3])
	_, err = client.Output(context.Background(), protocol.OutputRequest{
		Op: protocol.OpOutput, Name: "controls", Cwd: root, After: &after,
		Stream: protocol.StreamStdout, MaxEntries: 1, MaxBytes: len("fit"),
	})
	var wireErr *protocol.WireError
	if !errors.As(err, &wireErr) || wireErr.Code != protocol.ErrorOutput {
		t.Fatalf("ANSI-heavy bounded read error = %v, want output error for raw cursor %d", err, heavyCursor)
	}

	follow, err := client.Follow(context.Background(), protocol.FollowRequest{
		Op: protocol.OpFollow, Name: "controls", Cwd: root, Stream: protocol.StreamBoth,
		Match: `^ready`, MaxEntries: 16, MaxBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer follow.Close()
	initial, err := follow.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if initial.Read == nil || len(initial.Read.Entries) != 1 || initial.Read.Entries[0].Cursor != 0 || initial.Read.Entries[0].Text != stdoutRaw {
		t.Fatalf("raw initial follow event = %#v, want selected raw stdout entry", initial)
	}
	lateRaw := "\x1b[34mready-later\x1b[0m\r\n"
	lateCursor, err := store.Append(output.Stderr, at, lateRaw)
	if err != nil {
		t.Fatal(err)
	}
	later, err := follow.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if later.Read == nil || len(later.Read.Entries) != 1 || later.Read.Entries[0].Cursor != lateCursor || later.Read.Entries[0].Text != lateRaw {
		t.Fatalf("raw later follow event = %#v, want selected raw stderr entry", later)
	}
	if strings.Contains(response.Entries[0].Text, "\x1b") {
		t.Fatal("bounded child response retained ESC")
	}
}
