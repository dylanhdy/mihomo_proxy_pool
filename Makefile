LOCAL_IMAGE := mihomo-proxy-pool
REMOTE_IMAGE := crpi-tyce9o8tisqe9vrd.cn-shanghai.personal.cr.aliyuncs.com/crazy_david/mihomo-proxy-pool
TAG ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

.PHONY: build push

build:
	docker build . -t $(LOCAL_IMAGE):$(TAG)
	docker tag $(LOCAL_IMAGE):$(TAG) $(REMOTE_IMAGE):$(TAG)

push:
	docker push $(REMOTE_IMAGE):$(TAG)
