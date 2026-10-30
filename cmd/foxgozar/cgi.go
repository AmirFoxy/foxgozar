package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cgi"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"

	"foxgozar/internal/state"
)

// serveCGI is run by uhttpd as /cgi-bin/foxgozar: it hands the request to
// the API on 127.0.0.1. The web UI then talks to its own origin, so it works
// over both http and https and the API never listens on the network.
func serveCGI() {
	port := state.Defaults().APIPort
	if b, err := os.ReadFile(filepath.Join(paths.Etc, "state.json")); err == nil {
		var st struct {
			Settings struct {
				APIPort int `json:"api_port"`
			} `json:"settings"`
		}
		if json.Unmarshal(b, &st) == nil && st.Settings.APIPort > 0 {
			port = st.Settings.APIPort
		}
	}
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.URL.Path = os.Getenv("PATH_INFO")
			r.Out.URL.RawPath = ""
			r.Out.URL.RawQuery = os.Getenv("QUERY_STRING")
		},
		FlushInterval: -1, // live traffic is a stream
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, `{"error":"FoxGozar service is not running"}`)
		},
	}
	if err := cgi.Serve(proxy); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
