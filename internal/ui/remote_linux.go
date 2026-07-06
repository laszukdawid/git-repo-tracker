//go:build linux

package ui

import (
	"fyne.io/fyne/v2"
	"github.com/godbus/dbus/v5"
)

const (
	remoteBusName = "com.github.laszukdawid.GitRepoTracker"
	remotePath    = dbus.ObjectPath("/com/github/laszukdawid/GitRepoTracker")
	remoteIface   = remoteBusName
)

type remoteControl struct {
	app *App
}

func TryHandleRemote(opts RunOptions) bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	return conn.Object(remoteBusName, remotePath).Call(remoteIface+"."+remoteMethod(opts), 0).Err == nil
}

func (a *App) claimRemote(opts RunOptions) bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return true
	}
	reply, err := conn.RequestName(remoteBusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		_ = conn.Close()
		return true
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		return !TryHandleRemote(opts)
	}
	if err := conn.Export(remoteControl{app: a}, remotePath, remoteIface); err != nil {
		_ = conn.Close()
		return true
	}
	a.remoteCloser = conn
	return true
}

func remoteMethod(opts RunOptions) string {
	if opts.ShowSettings {
		return "ShowSettings"
	}
	return "OpenApp"
}

func (r remoteControl) OpenApp() *dbus.Error {
	fyne.Do(r.app.openApp)
	return nil
}

func (r remoteControl) ShowSettings() *dbus.Error {
	fyne.Do(r.app.showSettings)
	return nil
}
