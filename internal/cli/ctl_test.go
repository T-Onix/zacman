package cli

import (
	"strings"
	"testing"
)

func TestSystemctlPassthroughArgs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"ctl", "restart", "sshd"}, ""},
		{[]string{"ctl", "enable", "--now", "sshd"}, ""},
		{[]string{"ctl", "start", "--no-block", "sshd"}, ""},
		{[]string{"ctl", "status", "--full", "sshd"}, "status --full sshd"},
		{[]string{"ctl", "--user", "list-units"}, "--user list-units"},
		{[]string{"ctl", "mask", "foo.service"}, "mask foo.service"},
		{[]string{"--config", "/tmp/zacman.toml", "ctl", "daemon-reload"}, "daemon-reload"},
		{[]string{"diff"}, ""},
	}
	for _, tc := range cases {
		got, ok := systemctlPassthroughArgs(tc.args)
		if tc.want == "" {
			if ok {
				t.Errorf("%v: unexpected passthrough %v", tc.args, got)
			}
			continue
		}
		if !ok || strings.Join(got, " ") != tc.want {
			t.Errorf("%v: passthrough = %v (%v); want %q", tc.args, got, ok, tc.want)
		}
	}
}
