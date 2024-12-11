package main

import (
	"fmt"
	"github.com/docker/docker/pkg/reexec"
	gmux "github.com/gorilla/mux"
	caprconfigserver "github.com/rancher/cluster-api-provider-rancher/pkg/configserver"
	caprcontext "github.com/rancher/cluster-api-provider-rancher/pkg/context"
	"github.com/rancher/cluster-api-provider-rancher/pkg/controllers/bootstrap"
	"github.com/rancher/cluster-api-provider-rancher/pkg/controllers/machinenodelookup"
	"github.com/rancher/cluster-api-provider-rancher/pkg/controllers/plansecret"
	"github.com/rancher/cluster-api-provider-rancher/pkg/installer"
	"github.com/rancher/cluster-api-provider-rancher/pkg/settings"
	caprsettings "github.com/rancher/cluster-api-provider-rancher/pkg/settings"
	standalonekubeconfig "github.com/rancher/cluster-api-provider-rancher/pkg/standalone/controllers/kubeconfig"
	standalonerkecontrolplane "github.com/rancher/cluster-api-provider-rancher/pkg/standalone/controllers/rkecontrolplane"
	kcmanager "github.com/rancher/cluster-api-provider-rancher/pkg/standalone/kubeconfig"
	standaloneconfigserverresolvers "github.com/rancher/cluster-api-provider-rancher/pkg/standalone/resolvers/configserver"
	internalsettingsprovider "github.com/rancher/cluster-api-provider-rancher/pkg/standalone/settingsprovider"
	"github.com/rancher/dynamiclistener"
	dynamiclistenerserver "github.com/rancher/dynamiclistener/server"
	"github.com/rancher/dynamiclistener/storage/kubernetes"
	"github.com/rancher/dynamiclistener/storage/memory"
	_ "github.com/rancher/norman/controller"
	"github.com/rancher/norman/pkg/kwrapper/k8s"
	"github.com/rancher/wrangler/v3/pkg/signals"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/net"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
)

var (
	profileAddress   = "localhost:6060"
	kubeConfig       string
	serverURL        string
	port             int
	capiAPIServerURL string
)

func main() {
	if reexec.Init() {
		return
	}

	app := cli.NewApp()
	app.Version = "v0.0.0"
	app.Usage = "Complete container management platform"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "kubeconfig",
			Usage:       "Kube config for accessing the CAPI management k8s cluster",
			EnvVar:      "KUBECONFIG",
			Destination: &kubeConfig,
		},
		cli.StringFlag{
			Name:        "server-url",
			Usage:       "server URL for accessing the CAPR config server",
			EnvVar:      "SERVER_URL",
			Destination: &serverURL,
			Required:    true,
		},
		cli.IntFlag{
			Name:        "port",
			Usage:       "port for accessing the CAPR config server, if ",
			EnvVar:      "PORT",
			Destination: &port,
			Value:       7443,
		},
		cli.StringFlag{
			Name:        "capi-api-server-url",
			Usage:       "CAPI management cluster API server URL",
			EnvVar:      "CAPI_API_SERVER_URL",
			Destination: &capiAPIServerURL,
		},
	}

	app.Action = func(c *cli.Context) error {
		// enable profiler
		if profileAddress != "" {
			go func() {
				log.Println(http.ListenAndServe(profileAddress, nil))
			}()
		}
		initLogs()
		return run(c)
	}

	app.ExitErrHandler = func(c *cli.Context, err error) {
		logrus.Fatal(err)
	}

	app.Run(os.Args)
}

func initLogs() {
	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.TraceLevel)
	logrus.Tracef("Loglevel set to [%v]", logrus.TraceLevel)
}

type installerHandler struct{}

func (i *installerHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ca := ""
	installer.ServeHTTPWithChecksum(rw, req, ca)
}

var iHandler *installerHandler

func run(c *cli.Context) error {
	logrus.Info("Standalone cluster-api-provider-rancher is starting")
	ctx := signals.SetupSignalContext()

	// TODO: figure out if we can actually use the embedded K3s to run a mgmt CAPI cluster lol
	_, clientConfig, err := k8s.GetConfig(ctx, "auto", kubeConfig)
	if err != nil {
		return err
	}

	os.Unsetenv("KUBECONFIG")

	wContext, err := caprcontext.NewContext(ctx, clientConfig)
	if err != nil {
		return err
	}

	internalsettingsprovider.Register(wContext)

	settings.ServerURL.Set(serverURL)

	if capiAPIServerURL != "" {
		settings.CAPIAPIServerURL.Set(capiAPIServerURL)
	} else {
		if cc, err := clientConfig.ClientConfig(); err != nil || cc == nil {
			return fmt.Errorf("unable to grab API server URL from clientconfig: %v", err)
		} else {
			settings.CAPIAPIServerURL.Set(cc.Host)
		}
	}

	bootstrap.Register(wContext)

	kcManager := kcmanager.New(wContext)
	machinenodelookup.Register(wContext, kcManager)

	standalonerkecontrolplane.Register(wContext)

	plansecret.Register(wContext)

	standalonekubeconfig.Register(wContext)

	if err := wContext.Start(3); err != nil {
		logrus.Fatalf("Error starting: %s", err.Error())
	}
	mux := gmux.NewRouter()
	mux.UseEncodedPath()
	mux.Handle(installer.SystemAgentInstallPath, iHandler)
	mux.Handle("/healthz", healthz)
	caH := &caHandler{
		wContext: wContext,
	}
	mux.Handle("/cacerts", caH)
	mux.PathPrefix("/assets").Handler(http.FileServer(http.Dir(caprsettings.UIPath.Get())))

	caprConfigServerResolvers := standaloneconfigserverresolvers.NewStandaloneConfigServerResolver(wContext)
	caprConfigServer := caprconfigserver.New(wContext, caprConfigServerResolvers)

	mux.Handle(caprconfigserver.ConnectAgent, caprConfigServer)

	sans := []string{"localhost", "127.0.0.1", "capr.kube-system"}
	ip, err := net.ChooseHostInterface()
	if err == nil {
		sans = append(sans, ip.String())
	}

	serverOptions := &dynamiclistenerserver.ListenOpts{
		Storage:       kubernetes.Load(ctx, wContext.Core.Secret(), "kube-system", "tls-capr", memory.New()),
		Secrets:       wContext.Core.Secret(),
		CAName:        internalsettingsprovider.TlsCAName,
		CANamespace:   internalsettingsprovider.TlsCANamespace,
		CertNamespace: "kube-system",
		CertName:      "tls-capr",
		TLSListenerConfig: dynamiclistener.Config{
			SANs:                  sans,
			CloseConnOnCertChange: true,
		},
	}

	if err = dynamiclistenerserver.ListenAndServe(ctx, port, 0, mux, serverOptions); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	<-ctx.Done()
	return nil
}

type caHandler struct {
	wContext *caprcontext.Context
}

func (c *caHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ca := settings.CACerts.Get()

	rw.Header().Set("Content-Type", "text/plain")
	var bytes []byte
	if strings.TrimSpace(ca) != "" {
		if !strings.HasSuffix(ca, "\n") {
			ca += "\n"
		}
		bytes = []byte(ca)
	}

	if len(bytes) > 0 {
		_, _ = rw.Write([]byte(ca))
	}
}

type healthZHandler struct{}

func (h *healthZHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	_, _ = rw.Write([]byte("ok"))
}

var healthz *healthZHandler
