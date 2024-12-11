package installer

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/rancher/cluster-api-provider-rancher/pkg/settings"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

const (
	SystemAgentInstallPath = "/system-agent-install.sh" // corresponding curl -o in package/Dockerfile
	WindowsRke2InstallPath = "/wins-agent-install.ps1"  // corresponding curl -o in package/Dockerfile
)

func installScript(setting settings.Setting, files []string) ([]byte, error) {
	logrus.Debugf("setting was %s", setting.Get())
	if setting.Get() == setting.Default {
		// no setting override, check for local file first
		for _, f := range files {
			script, err := ioutil.ReadFile(f)
			if err != nil {
				if !os.IsNotExist(err) {
					logrus.Debugf("error pulling system agent installation script %s: %s", f, err)
				}
				continue
			}
			return script, err
		}
		logrus.Debugf("no local installation script found, moving on to url: %s", setting.Get())
	}

	resp, err := http.Get(setting.Get())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}

func LinuxInstallScript(token string, envVars []corev1.EnvVar, serverURL, ca, _ string) ([]byte, error) {
	data, err := installScript(
		settings.SystemAgentInstallScript,
		[]string{
			settings.UIPath.Get() + "/assets" + SystemAgentInstallPath,
			"." + SystemAgentInstallPath,
		})
	if err != nil {
		return nil, err
	}
	binaryURL := ""
	if settings.SystemAgentVersion.Get() != "" && serverURL != "" {
		binaryURL = fmt.Sprintf("CATTLE_AGENT_BINARY_BASE_URL=\"%s/assets\"", serverURL)
	}

	if ca != "" {
		ca = "CATTLE_CA_CHECKSUM=\"" + ca + "\""
	}

	if token != "" {
		token = "CATTLE_ROLE_NONE=true\nCATTLE_TOKEN=\"" + token + "\""
	}

	// Merge the env vars with the AgentTLSModeStrict
	found := false
	for _, ev := range envVars {
		if ev.Name == "STRICT_VERIFY" {
			found = true // The user has specified `STRICT_VERIFY`, we should not attempt to overwrite it.
		}
	}
	if !found {
		if settings.AgentTLSMode.Get() == "strict" {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "STRICT_VERIFY",
				Value: "true",
			})
		} else {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "STRICT_VERIFY",
				Value: "false",
			})
		}
	}

	envVarBuf := &strings.Builder{}
	for _, envVar := range envVars {
		if envVar.Value == "" {
			continue
		}
		envVarBuf.WriteString(fmt.Sprintf("%s=\"%s\"\n", envVar.Name, envVar.Value))
	}
	server := ""
	if settings.ServerURL.Get() != "" {
		server = fmt.Sprintf("CATTLE_SERVER=%s", settings.ServerURL.Get())
	}
	return []byte(fmt.Sprintf(`#!/usr/bin/env sh
%s
%s
%s
%s
%s

%s
`, envVarBuf.String(), binaryURL, server, ca, token, data)), nil
}

func WindowsInstallScript(token string, envVars []corev1.EnvVar, serverURL, ca, dataDir string) ([]byte, error) {
	data, err := installScript(
		settings.WinsAgentInstallScript,
		[]string{
			settings.UIPath.Get() + "/assets" + WindowsRke2InstallPath,
			"." + WindowsRke2InstallPath})
	if err != nil {
		return nil, err
	}

	binaryURL := ""
	if settings.WinsAgentVersion.Get() != "" && serverURL != "" {
		binaryURL = fmt.Sprintf("$env:CATTLE_AGENT_BINARY_BASE_URL=\"%s/assets\"", serverURL)
	}

	csiProxyURL := settings.CSIProxyAgentURL.Get()
	csiProxyVersion := "v1.0.0"
	if ver := settings.CSIProxyAgentVersion.Get(); ver != "" {
		csiProxyVersion = ver
		if serverURL != "" {
			csiProxyURL = fmt.Sprintf("%s/assets/csi-proxy-%%[1]s.tar.gz", serverURL)
		}
	}

	if ca != "" {
		ca = "$env:CATTLE_CA_CHECKSUM=\"" + ca + "\""
	}
	if token != "" {
		token = "$env:CATTLE_ROLE_NONE=\"true\"\n$env:CATTLE_TOKEN=\"" + token + "\""
	}
	envVarBuf := &strings.Builder{}
	for _, envVar := range envVars {
		if envVar.Value == "" {
			continue
		}
		envVarBuf.WriteString(fmt.Sprintf("$env:%s=\"%s\"\n", envVar.Name, envVar.Value))
	}
	server := ""
	if serverURL != "" {
		server = fmt.Sprintf("$env:CATTLE_SERVER=\"%s\"", serverURL)
	}

	strictVerify := "false"
	if settings.AgentTLSMode.Get() == settings.AgentTLSModeStrict {
		strictVerify = "true"
	}

	return []byte(fmt.Sprintf(`%s

%s
%s
%s
%s
%s

# Enables CSI Proxy
$env:CSI_PROXY_URL = "%s"
$env:CSI_PROXY_VERSION = "%s"
$env:CSI_PROXY_KUBELET_PATH = "C:%s/bin/kubelet.exe"
$env:STRICT_VERIFY = "%s"

Invoke-WinsInstaller @PSBoundParameters
exit 0
`, data, envVarBuf.String(), binaryURL, server, ca, token, csiProxyURL, csiProxyVersion, dataDir, strictVerify)), nil
}
