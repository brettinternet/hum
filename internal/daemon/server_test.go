package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"hum/internal/protocol"
)

func TestSinceProtocolVersionNegotiation(t *testing.T) {
	server := testServer(t, Config{WireVersion: protocol.Version - 1})
	client, err := Dial(context.Background(), server.Paths().Socket)
	if client == nil {
		t.Fatalf("legacy daemon dial returned nil client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	var mismatch *VersionMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("since request against legacy daemon hello error = %v, want version mismatch", err)
	}
	if mismatch.ClientVersion != protocol.Version || mismatch.DaemonVersion != protocol.Version-1 {
		t.Fatalf("since protocol mismatch = client %d daemon %d, want client %d daemon %d", mismatch.ClientVersion, mismatch.DaemonVersion, protocol.Version, protocol.Version-1)
	}
	_, outputErr := client.Output(context.Background(), protocol.OutputRequest{
		Op:            protocol.OpOutput,
		Name:          "api",
		Cwd:           t.TempDir(),
		SinceUnixNano: time.Now().Add(-time.Second).UnixNano(),
	})
	if !errors.As(outputErr, &mismatch) {
		t.Fatalf("since output against legacy daemon = %v, want cached version mismatch", outputErr)
	}
	if err := client.Shutdown(context.Background(), protocol.NewShutdownRequest(false)); err != nil {
		t.Fatalf("legacy daemon shutdown: %v", err)
	}
	if err := server.Wait(); err != nil {
		t.Fatalf("legacy daemon wait: %v", err)
	}
}
