package workbench

import (
	"net/url"
	"strings"

	"yovoice/internal/download"
)

func catalogParseProxy(address string) *url.URL {
	address = strings.TrimSpace(address)
	if address != "" && !strings.Contains(address, "://") {
		address = "http://" + address
	}
	proxy, err := download.ParseProxy(address)
	if err != nil {
		return nil
	}
	return proxy
}
