IMAGE_REPO ?= oats87
IMAGE ?= cluster-api-provider-rancher
IMAGE_TAG ?= dev

build:
	docker build -t ${IMAGE_REPO}/${IMAGE}:${IMAGE_TAG} -f package/Dockerfile .

run-dev:
	docker run -v ./kubeconfig:/kubeconfig --network host ${IMAGE_REPO}/${IMAGE}:${IMAGE_TAG} --kubeconfig=/kubeconfig --server-url=https://192.168.47.151:7443 --capi-api-server-url=https://192.168.47.151:9443
