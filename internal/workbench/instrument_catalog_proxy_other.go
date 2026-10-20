//go:build !windows

package workbench

import "net/url"

func catalogSystemProxy() *url.URL { return nil }
