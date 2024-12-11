package configserver

import "net/http"

type Resolver interface {
	GetCorrespondingMachineByRequest(req *http.Request) (string, string, error)
	GetK8sAPIServerURLAndCertificateByRequest(req *http.Request) (string, []byte)
	Ready() (bool, error)
}
