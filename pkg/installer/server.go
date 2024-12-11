package installer

import (
	"fmt"
	caprsettings "github.com/rancher/cluster-api-provider-rancher/pkg/settings"
	"net/http"
)

func ServeHTTPWithChecksum(rw http.ResponseWriter, req *http.Request, ca string) {
	serverURL := caprsettings.ServerURL.Get()

	if serverURL == "" {
		serverURL = fmt.Sprintf("https://%s", req.Host)
	}

	var err error
	var content []byte
	switch req.URL.Path {
	case SystemAgentInstallPath:
		content, err = LinuxInstallScript("", nil, serverURL, ca, "")
	case WindowsRke2InstallPath:
		content, err = WindowsInstallScript("", nil, serverURL, ca, "")
	}

	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/plain")
	rw.Write(content)
}
