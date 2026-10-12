//go:build linux

package webview

import (
	native "wagramdesklite/internal/webview/native"
)

type Hint int

const HintNone Hint = 0

func New(debug bool) WebView { return native.New(debug) }
