SHELL := /usr/bin/env bash

.tag:
	echo $(shell git rev-parse --short HEAD) >| .tag

build: .tag
	docker build . -t xoxodev.azurecr.io/route-table-syncer:$(shell cat .tag)

push: .tag
	docker push xoxodev.azurecr.io/route-table-syncer:$(shell cat .tag)

push-non-dev: 
	docker tag xoxodev.azurecr.io/route-table-syncer:$(shell cat .tag) xoxoint.azurecr.io/route-table-syncer:$(shell cat .tag)
	docker tag xoxodev.azurecr.io/route-table-syncer:$(shell cat .tag) xoxoprod.azurecr.io/route-table-syncer:$(shell cat .tag)
	docker push xoxoint.azurecr.io/route-table-syncer:$(shell cat .tag)
	docker push xoxoprod.azurecr.io/route-table-syncer:$(shell cat .tag)

deploy:
	./deploy.sh

deploy-for-real:
	./deploy.sh --real-run

clean:
	rm -f .tag
