package network

import "testing"

func TestAllowAndDeny(t *testing.T) {
	for _, tt := range []struct {
		allow, deny []string
		want        bool
	}{
		{nil, nil, true}, {[]string{"eth0"}, nil, true}, {[]string{"wg0"}, nil, false}, {[]string{"eth0"}, []string{"eth0"}, false}, {nil, []string{"eth0"}, false},
	} {
		if got := Enabled("eth0", tt.allow, tt.deny); got != tt.want {
			t.Fatalf("%+v got %v", tt, got)
		}
	}
}
