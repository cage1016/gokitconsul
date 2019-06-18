package gokitconsul

import (
	"encoding/json"
	"net/http"
)

const version string = "0.1.0"

type VersionInfo struct {
	Service string `json:"service"`
	Version string `json:"version"`
}

// Version exposes an HTTP handler for retrieving service version.
func Version(service string) http.HandlerFunc {
	return http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		res := VersionInfo{service, version}

		data, _ := json.Marshal(res)

		rw.Write(data)
	})
}
