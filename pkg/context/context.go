package context

import (
	"context"
	rkev1 "github.com/rancher/cluster-api-provider-rancher/pkg/apis/rke.cattle.io/v1"
	capi "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/cluster.x-k8s.io"
	capicontrollers "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/cluster.x-k8s.io/v1beta1"
	rke "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/rke.cattle.io"
	rkecontrollers "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/rke.cattle.io/v1"
	"github.com/rancher/lasso/pkg/controller"
	"github.com/rancher/lasso/pkg/dynamic"
	"github.com/rancher/wrangler/v3/pkg/apply"
	app "github.com/rancher/wrangler/v3/pkg/generated/controllers/apps"
	appcontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/apps/v1"
	core "github.com/rancher/wrangler/v3/pkg/generated/controllers/core"
	corecontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/rancher/wrangler/v3/pkg/generated/controllers/rbac"
	rbaccontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/rbac/v1"
	"github.com/rancher/wrangler/v3/pkg/generic"
	"github.com/rancher/wrangler/v3/pkg/schemes"
	"github.com/sirupsen/logrus"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	apiregistrationv12 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	capiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"time"
)

var (
	localSchemeBuilder = runtime.SchemeBuilder{
		capiv1beta1.AddToScheme,
		rkev1.AddToScheme,
		scheme.AddToScheme,
		apiextensionsv1.AddToScheme,
		apiregistrationv12.AddToScheme,
	}
	AddToScheme = localSchemeBuilder.AddToScheme
	Scheme      = runtime.NewScheme()
)

type Context struct {
	Ctx        context.Context
	RESTConfig *rest.Config
	K8s        *kubernetes.Clientset
	Apply      apply.Apply
	Dynamic    *dynamic.Controller

	Core corecontrollers.Interface
	RKE  rkecontrollers.Interface
	CAPI capicontrollers.Interface
	Apps appcontrollers.Interface
	RBAC rbaccontrollers.Interface

	SharedControllerFactory controller.SharedControllerFactory
}

func init() {
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})
	utilruntime.Must(AddToScheme(Scheme))
	utilruntime.Must(schemes.AddToScheme(Scheme))
}

func enableProtobuf(cfg *rest.Config) *rest.Config {
	cpy := rest.CopyConfig(cfg)
	cpy.AcceptContentTypes = "application/vnd.kubernetes.protobuf, application/json"
	cpy.ContentType = "application/json"
	return cpy
}

func NewContext(ctx context.Context, cc clientcmd.ClientConfig) (*Context, error) {
	restConfig, err := cc.ClientConfig()
	restConfig.Timeout = 10 * time.Minute

	controllerFactory, err := controller.NewSharedControllerFactoryFromConfigWithOptions(enableProtobuf(restConfig), Scheme, nil)
	if err != nil {
		return nil, err
	}

	var c Context
	c.Ctx = ctx
	opts := &generic.FactoryOptions{
		SharedControllerFactory: controllerFactory,
		SharedCacheFactory:      controllerFactory.SharedCacheFactory(),
	}

	c.RESTConfig = restConfig

	k8s, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	c.K8s = k8s

	apply, err := apply.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	c.Apply = apply

	dynamic := dynamic.New(k8s.Discovery())
	c.Dynamic = dynamic

	if err := c.Dynamic.Register(c.Ctx, controllerFactory); err != nil {
		return nil, err
	}

	core, err := core.NewFactoryFromConfigWithOptions(restConfig, opts)
	if err != nil {
		return nil, err
	}
	c.Core = core.Core().V1()

	rke, err := rke.NewFactoryFromConfigWithOptions(restConfig, opts)
	if err != nil {
		return nil, err
	}
	c.RKE = rke.Rke().V1()

	capi, err := capi.NewFactoryFromConfigWithOptions(restConfig, opts)
	if err != nil {
		return nil, err
	}
	c.CAPI = capi.Cluster().V1beta1()

	apps, err := app.NewFactoryFromConfigWithOptions(restConfig, opts)
	if err != nil {
		return nil, err
	}
	c.Apps = apps.Apps().V1()

	rbac, err := rbac.NewFactoryFromConfigWithOptions(restConfig, opts)
	if err != nil {
		return nil, err
	}
	c.RBAC = rbac.Rbac().V1()

	c.SharedControllerFactory = controllerFactory

	return &c, nil
}

func (w *Context) Start(threadiness int) error {
	logrus.Infof("Starting shared cache factory")
	if err := w.SharedControllerFactory.SharedCacheFactory().Start(w.Ctx); err != nil {
		return err
	}
	logrus.Infof("Started shared cache factory")
	w.SharedControllerFactory.SharedCacheFactory().WaitForCacheSync(w.Ctx)

	logrus.Infof("Starting controller factory")
	return w.SharedControllerFactory.Start(w.Ctx, threadiness)
}
