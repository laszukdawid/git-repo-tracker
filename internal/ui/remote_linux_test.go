//go:build linux

package ui

import "testing"

func TestRemoteMethod(t *testing.T) {
	tests := []struct {
		name string
		opts RunOptions
		want string
	}{
		{name: "open app", want: "OpenApp"},
		{name: "show settings", opts: RunOptions{ShowSettings: true}, want: "ShowSettings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := remoteMethod(tt.opts); got != tt.want {
				t.Fatalf("remoteMethod(%+v) = %q, want %q", tt.opts, got, tt.want)
			}
		})
	}
}
