package workbench

import (
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func catalogSystemProxy() *url.URL {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil
	}
	address, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return nil
	}
	for _, part := range strings.Split(address, ";") {
		if strings.HasPrefix(part, "https=") {
			return catalogParseProxy(strings.TrimPrefix(part, "https="))
		}
		if !strings.Contains(part, "=") {
			return catalogParseProxy(part)
		}
	}
	return nil
}
