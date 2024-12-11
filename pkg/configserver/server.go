package configserver

import (
	"encoding/json"
	"fmt"
	caprcontext "github.com/rancher/cluster-api-provider-rancher/pkg/context"
	"net/http"
	"time"

	"github.com/rancher/cluster-api-provider-rancher/pkg"
	capicontrollers "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/cluster.x-k8s.io/v1beta1"
	rkecontroller "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/rke.cattle.io/v1"
	corecontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
)

const (
	ConnectAgent = "/v3/connect/agent"
)

type CAPRConfigServer struct {
	serviceAccountsCache corecontrollers.ServiceAccountCache
	serviceAccounts      corecontrollers.ServiceAccountClient
	secretsCache         corecontrollers.SecretCache
	secrets              corecontrollers.SecretController
	machineCache         capicontrollers.MachineCache
	machines             capicontrollers.MachineClient
	bootstrapCache       rkecontroller.RKEBootstrapCache
	k8s                  kubernetes.Interface
	resolver             Resolver
}

func New(wContext *caprcontext.Context, resolver Resolver) *CAPRConfigServer {
	return &CAPRConfigServer{
		serviceAccountsCache: wContext.Core.ServiceAccount().Cache(),
		serviceAccounts:      wContext.Core.ServiceAccount(),
		secretsCache:         wContext.Core.Secret().Cache(),
		secrets:              wContext.Core.Secret(),
		machineCache:         wContext.CAPI.Machine().Cache(),
		machines:             wContext.CAPI.Machine(),
		bootstrapCache:       wContext.RKE.RKEBootstrap().Cache(),
		k8s:                  wContext.K8s,
		resolver:             resolver,
	}
}

func (r *CAPRConfigServer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ready, err := r.resolver.Ready()
	if err != nil || !ready {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}
	planSecret, secret, err := r.findSA(req)
	if apierrors.IsNotFound(err) {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	} else if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	} else if secret == nil || secret.Data[corev1.ServiceAccountTokenKey] == nil {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}

	switch req.URL.Path {
	case ConnectAgent:
		r.connectAgent(planSecret, secret, rw, req)
	}
}

func (r *CAPRConfigServer) connectAgent(planSecret string, secret *corev1.Secret, rw http.ResponseWriter, req *http.Request) {
	url, _ := r.resolver.GetK8sAPIServerURLAndCertificateByRequest(req)

	if url == "" {
		http.Error(rw, "unable to determine API Server URL", http.StatusInternalServerError)
		return
	}

	kubeConfig, err := clientcmd.Write(clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"agent": {
				Server:                url,
				InsecureSkipTLSVerify: true,
				//CertificateAuthorityData: ca,
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"agent": {
				Token: string(secret.Data["token"]),
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"agent": {
				Cluster:  "agent",
				AuthInfo: "agent",
			},
		},
		CurrentContext: "agent",
	})
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(rw)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]string{
		"namespace":  secret.Namespace,
		"secretName": planSecret,
		"kubeConfig": string(kubeConfig),
	})
}

func (r *CAPRConfigServer) findMachineByID(machineID, ns string) (*capi.Machine, error) {
	machines, err := r.machineCache.List(ns, labels.SelectorFromSet(map[string]string{
		capr.MachineIDLabel: machineID,
	}))
	if err != nil {
		return nil, err
	}

	if len(machines) != 1 {
		return nil, fmt.Errorf("unable to find machine %s, found %d machine(s)", machineID, len(machines))
	}

	return machines[0], nil
}

// findSA uses the request machineID to find and deliver the plan secret name and a service account token (or an error).
func (r *CAPRConfigServer) findSA(req *http.Request) (string, *corev1.Secret, error) {
	machineID := req.Header.Get(capr.MachineIDHeader)
	logrus.Debugf("[rke2configserver] parsed %s as machineID", machineID)
	if machineID == "" {
		return "", nil, nil
	}

	machineNamespace, machineName, err := r.resolver.GetCorrespondingMachineByRequest(req)
	if err != nil {
		return "", nil, err
	}
	logrus.Debugf("[rke2configserver] Got %s/%s machine", machineNamespace, machineName)

	if machineName == "" || machineNamespace == "" {
		return "", nil, fmt.Errorf("machine not found by request")
	}

	if err := r.setOrUpdateMachineID(machineNamespace, machineName, machineID); err != nil {
		return "", nil, err
	}

	planSAs, err := r.serviceAccountsCache.List(machineNamespace, labels.SelectorFromSet(map[string]string{
		capr.MachineNameLabel: machineName,
		capr.RoleLabel:        capr.RolePlan,
	}))
	if err != nil {
		return "", nil, err
	}

	logrus.Debugf("[rke2configserver] %s/%s listed %d planSAs", machineNamespace, machineName, len(planSAs))

	for _, planSA := range planSAs {
		if err := capr.PlanSACheck(r.bootstrapCache, machineName, planSA); err != nil {
			logrus.Errorf("[rke2configserver] error encountered when searching for checking planSA %s/%s against machine %s: %v", planSA.Namespace, planSA.Name, machineName, err)
			continue
		}
		planSecret, err := capr.GetPlanSecretName(planSA)
		if err != nil {
			logrus.Errorf("[rke2configserver] error encountered when searching for plan secret name for planSA %s/%s: %v", planSA.Namespace, planSA.Name, err)
			continue
		}
		logrus.Debugf("[rke2configserver] %s/%s plan secret was %s", machineNamespace, machineName, planSecret)
		if planSecret == "" {
			continue
		}
		tokenSecret, _, err := capr.GetPlanServiceAccountTokenSecret(r.secrets, r.k8s, planSA)
		if err != nil {
			logrus.Errorf("[rke2configserver] error encountered when searching for token secret for planSA %s/%s: %v", planSA.Namespace, planSA.Name, err)
			continue
		}
		if tokenSecret == nil {
			logrus.Debugf("[rke2configserver] %s/%s token secret for planSecret %s was nil", machineNamespace, machineName, planSecret)
			continue
		}
		logrus.Infof("[rke2configserver] %s/%s machineID: %s delivering planSecret %s with token secret %s/%s to system-agent", machineNamespace, machineName, machineID, planSecret, tokenSecret.Namespace, tokenSecret.Name)
		return planSecret, tokenSecret, err
	}

	logrus.Debugf("[rke2configserver] %s/%s watching for plan secret to become ready for consumption", machineNamespace, machineName)

	// The plan service account will likely not exist yet -- the plan service account is created by the bootstrap controller.
	respSA, err := r.serviceAccounts.Watch(machineNamespace, metav1.ListOptions{
		LabelSelector: capr.MachineNameLabel + "=" + machineName + "," + capr.RoleLabel + "=" + capr.RolePlan,
	})
	if err != nil {
		return "", nil, err
	}
	defer func() {
		respSA.Stop()
		for range respSA.ResultChan() {
		}
	}()

	// The following logic will start a watch for plan service accounts --
	// once we see the first valid plan service account come through, we then will open a watch for secrets to look for the corresponding secret for that plan service account.
	var planSA *corev1.ServiceAccount
	var planSecret string

	for event := range respSA.ResultChan() {
		var ok bool
		if planSA, ok = event.Object.(*corev1.ServiceAccount); ok {
			if err := capr.PlanSACheck(r.bootstrapCache, machineName, planSA); err != nil {
				logrus.Errorf("[rke2configserver] error encountered when searching for checking planSA %s/%s against machine %s: %v", planSA.Namespace, planSA.Name, machineName, err)
				continue
			}
			planSecret, err = capr.GetPlanSecretName(planSA)
			if err != nil {
				logrus.Errorf("[rke2configserver] error encountered when searching for plan secret name for planSA %s/%s: %v", planSA.Namespace, planSA.Name, err)
				continue
			}
			logrus.Debugf("[rke2configserver] %s/%s plan secret was %s", machineNamespace, machineName, planSecret)
			if planSecret == "" {
				continue
			}
			tokenSecret, watchable, err := capr.GetPlanServiceAccountTokenSecret(r.secrets, r.k8s, planSA)
			if err != nil || tokenSecret == nil {
				logrus.Debugf("[rke2configserver] %s/%s token secret for planSecret %s was nil or error received", machineNamespace, machineName, planSecret)
				if err != nil {
					logrus.Errorf("[rke2configserver] error encountered when searching for token secret for planSA %s/%s: %v", planSA.Namespace, planSA.Name, err)
				}
				if watchable {
					logrus.Debugf("[rke2configserver] %s/%s token secret for planSecret %s is watchable, starting secret watch to wait for token to populate", machineNamespace, machineName, planSecret)
					break
				}
				continue
			}
			logrus.Infof("[rke2configserver] %s/%s machineID: %s delivering planSecret %s with token secret %s/%s to system-agent from plan service account watch", machineNamespace, machineName, machineID, planSecret, tokenSecret.Namespace, tokenSecret.Name)
			return planSecret, tokenSecret, nil
		}
	}

	if planSecret == "" || planSA == nil {
		return "", nil, fmt.Errorf("could not start secret watch for token secret")
	}

	logrus.Debugf("[rke2configserver] %s/%s starting token secret watch for planSA %s/%s", machineNamespace, machineName, planSA.Namespace, planSA.Name)
	// start watch for the planSA corresponding secret, using a label selector.
	respSecret, err := r.secrets.Watch(machineNamespace, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("%s=%s", "type", corev1.SecretTypeServiceAccountToken),
	})
	if err != nil {
		return "", nil, err
	}
	defer func() {
		respSecret.Stop()
		for range respSecret.ResultChan() {
		}
	}()
	for event := range respSecret.ResultChan() {
		if secret, ok := event.Object.(*corev1.Secret); ok {
			if secret.Annotations[corev1.ServiceAccountNameKey] != planSA.Name {
				continue
			}
			logrus.Infof("[rke2configserver] %s/%s machineID: %s delivering planSecret %s with token secret %s/%s to system-agent from secret watch", machineNamespace, machineName, machineID, planSecret, secret.Namespace, secret.Name)
			return planSecret, secret, nil
		}
	}

	return "", nil, fmt.Errorf("timeout waiting for plan")
}

func (r *CAPRConfigServer) setOrUpdateMachineID(machineNamespace, machineName, machineID string) error {
	machine, err := r.machineCache.Get(machineNamespace, machineName)
	if err != nil {
		return err
	}

	if machine.Labels[capr.MachineIDLabel] == machineID {
		return nil
	}

	machine = machine.DeepCopy()
	if machine.Labels == nil {
		machine.Labels = map[string]string{}
	}

	machine.Labels[capr.MachineIDLabel] = machineID
	_, err = r.machines.Update(machine)
	logrus.Debugf("[rke2configserver] %s/%s updated machine ID to %s", machineNamespace, machineName, machineID)
	return err
}

func (r *CAPRConfigServer) getMachinePlanSecret(ns, name string) (*corev1.Secret, error) {
	backoff := wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   2,
		Steps:    10,
		Cap:      2 * time.Second,
	}
	var secret *corev1.Secret
	return secret, wait.ExponentialBackoff(backoff, func() (bool, error) {
		var err error
		secret, err = r.secretsCache.Get(name, ns)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return false, err // hard error out if there's a problem
			}
			return false, nil // retry if secret not found
		}

		if len(secret.Data) == 0 || string(secret.Data[capr.RolePlan]) == "" {
			return false, nil // retry if no secret Data or plan, backoff and wait for the controller
		}

		return true, nil
	})
}
