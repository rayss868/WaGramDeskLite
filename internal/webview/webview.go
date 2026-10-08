// Package webview provides the small cross-platform WebView surface used by the app.
package webview

import "unsafe"

type WebView interface {
	Run()
	Terminate()
	Dispatch(func())
	Destroy()
	Window() unsafe.Pointer
	SetTitle(string)
	Navigate(string)
	Init(string)
	Eval(string)
	Bind(string, interface{}) error
}
