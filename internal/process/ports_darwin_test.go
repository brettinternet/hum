//go:build darwin

package process

import (
	"reflect"
	"testing"
)

func TestPortsLsofFieldParser(t *testing.T) {
	input := []byte("p41\nf3\ntIPv4\nd0xd1\nPTCP\nn*:8080\np42\nf3\ntIPv4\nd0xd1\nPTCP\nn*:8080\np43\nf4\ntIPv6\nd0xd2\nPTCP\nn[::1]:8081\n")
	listeners, err := parseLsofFields(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(listeners) != 3 {
		t.Fatalf("parsed listeners = %#v", listeners)
	}
	builder := make(portBuilder)
	for _, listener := range listeners {
		builder.add("lsof:"+listener.identity, listener.address, listener.port, listener.pid)
	}
	got := builder.listeners()
	if len(got) != 2 || !reflect.DeepEqual(got[0].PIDs, []int{41, 42}) || got[0].Address != "*" || got[0].Port != 8080 || got[1].Address != "::1" {
		t.Fatalf("aggregated listeners = %#v", got)
	}
}

func TestPortsLsofRequiresSocketIdentity(t *testing.T) {
	listeners, err := parseLsofFields([]byte("p41\nf3\ntIPv4\nPTCP\nn127.0.0.1:8080\n"))
	if err == nil || len(listeners) != 0 {
		t.Fatalf("parse without socket identity = %#v, %v; want diagnostic and no listener", listeners, err)
	}
	listeners, err = parseLsofFields([]byte("p41\nf3\ntIPv4\nd0xd1\nPTCP\nn127.0.0.1:8080\nf4\ntIPv4\nPTCP\nn127.0.0.1:8081\n"))
	if err == nil || len(listeners) != 1 || listeners[0].port != 8080 {
		t.Fatalf("descriptor without identity reused previous socket: %#v, %v", listeners, err)
	}
}

func FuzzPortsLsofFieldParser(f *testing.F) {
	f.Add([]byte("p41\nf3\ntIPv4\nd0xd1\nPTCP\nn*:8080\n"))
	f.Add([]byte("p42\nf4\ntIPv6\nd0xd2\nPTCP\nn[::1]:8081\n"))
	f.Add([]byte("p43\nf5\ntIPv4\nd0xd3\nPTCP\nn127.0.0.1:1->127.0.0.1:2\n"))
	f.Add([]byte("pbad\nf\ntIPv4\nd\nPTCP\nn:0\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		listeners, _ := parseLsofFields(data)
		for _, listener := range listeners {
			if listener.pid <= 0 || listener.identity == "" || listener.address == "" || listener.port == 0 {
				t.Fatalf("parser returned invalid listener %#v", listener)
			}
		}
	})
}
