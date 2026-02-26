build:
	docker build . -t mihomo-proxy-pool:dev
	docker tag mihomo-proxy-pool:dev crpi-tyce9o8tisqe9vrd.cn-shanghai.personal.cr.aliyuncs.com/crazy_david/mihomo-proxy-pool:dev

push:
	docker push crpi-tyce9o8tisqe9vrd.cn-shanghai.personal.cr.aliyuncs.com/crazy_david/mihomo-proxy-pool:dev
