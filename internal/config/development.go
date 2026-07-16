package config

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

func validEndpointScheme(development bool, endpoint *url.URL) bool {
	if endpoint.Scheme == "https" {
		return true
	}
	return development && endpoint.Scheme == "http" && isLoopbackHost(endpoint.Hostname())
}

// ValidateListenAddress prevents insecure development cookies from leaving loopback.
func (file File) ValidateListenAddress(address string) error {
	if !file.Development {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || !isLoopbackHost(host) {
		return errors.New("development mode must listen on an explicit loopback address")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
