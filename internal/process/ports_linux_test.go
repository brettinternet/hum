//go:build linux

package process

import (
	"reflect"
	"testing"
)

func TestPortsProcTableParser(t *testing.T) {
	tcp := []byte("  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 12345\n" +
		"   1: 0100007F:1F91 00000000:0000 01 00000000:00000000 00:00000000 00000000 1000 0 12346\n")
	rows, err := parseProcTCPTable(tcp, "tcp")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows["12345"] != (procListener{address: "127.0.0.1", port: 8080}) {
		t.Fatalf("parsed IPv4 listeners = %#v", rows)
	}

	tcp6 := []byte("sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n" +
		"0: 00000000000000000000000001000000:1F91 00000000000000000000000000000000:0000 0A 0 0 00:0 0 0 12347\n")
	rows, err = parseProcTCPTable(tcp6, "tcp6")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows["12347"] != (procListener{address: "::1", port: 8081}) {
		t.Fatalf("parsed IPv6 listeners = %#v", rows)
	}
}

func TestPortsSharedSocketAggregationKeepsEveryPID(t *testing.T) {
	builder := make(portBuilder)
	builder.add("net:[1]:42", "0.0.0.0", 5000, 27)
	builder.add("net:[1]:42", "0.0.0.0", 5000, 13)
	listeners := builder.listeners()
	if len(listeners) != 1 || listeners[0].Transport != "tcp" || !reflect.DeepEqual(listeners[0].PIDs, []int{13, 27}) {
		t.Fatalf("aggregated listeners = %#v, want one shared socket with sorted holders", listeners)
	}
	builder.add("net:[1]:43", "0.0.0.0", 5000, 41)
	if listeners = builder.listeners(); len(listeners) != 2 {
		t.Fatalf("distinct sockets on same endpoint collapsed: %#v", listeners)
	}
}
