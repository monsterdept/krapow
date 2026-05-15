package tart

import "testing"

func TestFindLeaseByMAC(t *testing.T) {
	// Real-shaped leases file with the exact malformed entry that broke us:
	// a bare `name` line inside an otherwise-valid block.
	leases := `{
	name=stale
	ip_address=192.168.64.5
	hw_address=1,ea:de:6:b9:36:b7
	identifier=1,ea:de:6:b9:36:b7
	lease=0x6851c5be
}
{
	name
	ip_address=192.168.64.6
	hw_address=1,32:f6:a1:76:6d:76
	identifier=1,32:f6:a1:76:6d:76
	lease=0x69f97b3a
}
{
	name=ours
	ip_address=192.168.64.13
	hw_address=1,06:e8:a3:b0:23:b4
	identifier=1,06:e8:a3:b0:23:b4
	lease=0x6a06ca16
}`

	cases := []struct {
		name, mac, want string
	}{
		{"zero-padded MAC matches dhcpd's shortened form", "06:e8:a3:b0:23:b4", "192.168.64.13"},
		{"shortened MAC matches zero-padded dhcpd entry", "6:e8:a3:b0:23:b4", "192.168.64.13"},
		{"block with bare key still parses other fields", "32:f6:a1:76:6d:76", "192.168.64.6"},
		{"no match returns empty, not error", "ff:ff:ff:ff:ff:ff", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := findLeaseByMAC(leases, c.mac)
			if got != c.want {
				t.Fatalf("findLeaseByMAC(%q) = %q, want %q", c.mac, got, c.want)
			}
		})
	}
}

func TestNormalizeMAC(t *testing.T) {
	cases := map[string]string{
		"06:E8:A3:B0:23:B4": "06:e8:a3:b0:23:b4",
		"6:e8:a3:b0:23:b4":  "06:e8:a3:b0:23:b4",
		"6:8:3:0:3:4":       "06:08:03:00:03:04",
	}
	for in, want := range cases {
		if got := normalizeMAC(in); got != want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", in, got, want)
		}
	}
}
