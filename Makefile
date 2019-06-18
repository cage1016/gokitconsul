BUILD_DIR = build
SERVICES = gateway addsvc
DOCKERS = $(addprefix docker_,$(SERVICES))
DOCKERS_DEV = $(addprefix docker_dev_,$(SERVICES))
REBUILD = $(addprefix rebuild_,$(SERVICES))
CGO_ENABLED ?= 0
GOOS ?= linux
# GOOS ?= darwin

define compile_service
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) go build -ldflags "-s -w" -o ${BUILD_DIR}/gokitconsul-$(1) cmd/$(1)/main.go
endef

define make_docker
	docker build --no-cache --build-arg SVC_NAME=$(subst docker_,,$(1)) --tag=cage1016/gokitconsul-$(subst docker_,,$(1)) -f deployments/docker/Dockerfile .
endef

define make_docker_dev
	docker build --build-arg SVC_NAME=$(subst docker_dev_,,$(1)) --tag=cage1016/gokitconsul-$(subst docker_dev_,,$(1)) -f deployments/docker/Dockerfile.dev ./build
endef

all: $(SERVICES)

.PHONY: all $(SERVICES) dockers dockers_dev

cleandocker: cleanghost
	# Stop all containers (if running)
	docker-compose -f deployments/docker/docker-compose.yaml stop
	# Remove gokitconsul containers
	docker ps -f name=gokitconsul -aq | xargs docker rm
	# Remove old gokitconsul images
	docker images -q cage1016/gokitconsul-* | xargs docker rmi

# Clean ghost docker images
cleanghost:
	# Remove exited containers
	docker ps -f status=dead -f status=exited -aq | xargs docker rm -v
	# Remove unused images
	docker images -f dangling=true -q | xargs docker rmi
	# Remove unused volumes
	docker volume ls -f dangling=true -q | xargs docker volume rm

install:
	cp ${BUILD_DIR}/* $(GOBIN)

test:
	go test -v -race -tags test $(shell go list ./... | grep -v 'vendor\|cmd')

PD_SOURCES:=$(shell find ./pb -type d)
proto:
	@for var in $(PD_SOURCES); do \
		if [ -f "$$var/compile.sh" ]; then \
			cd $$var && ./compile.sh; \
			echo "complie $$var/$$(basename $$var).proto"; \
			cd $(PWD); \
		fi \
	done

$(SERVICES):
	$(call compile_service,$(@))

$(DOCKERS):
	$(call make_docker,$(@))

services: $(SERVICES)

dockers: $(DOCKERS)

rebuilds: $(REBUILD)

$(REBUILD):
	$(call compile_service,$(subst rebuild_,,$(@)))
	$(call make_docker_dev,$(subst rebuild_,,$(@)))

$(DOCKERS_DEV):
	$(call make_docker_dev,$(@))

dockers_dev: $(DOCKERS_DEV)

u:
	docker-compose -f deployments/docker/docker-compose.yaml up -d

d:
	docker-compose -f deployments/docker/docker-compose.yaml down

log:
	docker logs -f gokitconsul-arithmetic

restart: d u

goaddsvc:
	QS_ADDSVC_LOG_LEVEL=info \
	QS_ADDSVC_HTTP_PORT=8020 \
	QS_ADDSVC_GRPC_PORT=8021 \
	QS_CONSULT_HOST=0.0.0.0 \
	QS_CONSULT_PORT=8500 \
	go run cmd/addsvc/main.go

gogateway:
	QS_GATEWAY_LOG_LEVEL=info \
	QS_GATEWAY_HTTP_PORT=9000 \
	QS_GATEWAY_CONSULT_HOST=0.0.0.0 \
	QS_GATEWAY_CONSULT_PORT=8500 \
	QS_GATEWAY_SERVER_CERT=./deployments/docker/ssl/localhost+3.pem \
	QS_GATEWAY_SERVER_KEY=./deployments/docker/ssl/localhost+3-key.pem \
	go run cmd/gateway/main.go
