<h1 align="center">Welcome to gokitconsul 👋</h1>
<p>
  <a href="https://github.com/cage1016/gokitconsul/blob/master/LICENSE">
    <img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-yellow.svg" target="_blank" />
  </a>
  <a href="https://twitter.com/CageChung">
    <img alt="Twitter: CageChung" src="https://img.shields.io/twitter/follow/CageChung.svg?style=social" target="_blank" />
  </a>
</p>

> go kit microservice demo with consul & zipkin

## Dependency

- consul as discover service
- zipkin as trace service
- gateway

## Services

- pkg/addsvc
- pkg/foosvc


## Install

```sh
# build docker image form bindary
make rebuilds

# clear build (options)
make dockers
```

## Usage

```sh
# start
$ make u
docker-compose -f deployments/docker/docker-compose.yaml up -d
Creating network "docker_gokitconsul_base_net" with driver "bridge"
Creating gokitconsul-zipkin ... done
Creating gokitconsul-consul ... done
Creating gokitconsul-addsvc ... done
Creating gokitconsul-foosvc ... done
Creating gokitconsul-gateway ... done
...


$ docker ps
CONTAINER ID        IMAGE                                 COMMAND                  CREATED             STATUS              PORTS                                                                                                            NAMES
57157cdc19dd        cage1016/gokitconsul-foosvc:latest    "/exe"                   10 minutes ago      Up 10 minutes                                                                                                                        gokitconsul-foosvc
29f808c19f2b        cage1016/gokitconsul-gateway:latest   "/exe"                   10 minutes ago      Up 10 minutes       0.0.0.0:8000->8000/tcp                                                                                           gokitconsul-gateway
6865dd7727c4        cage1016/gokitconsul-addsvc:latest    "/exe"                   10 minutes ago      Up 10 minutes                                                                                                                        gokitconsul-addsvc
71a8bdaba125        openzipkin/zipkin                     "/busybox/sh run.sh"     10 minutes ago      Up 10 minutes       9410/tcp, 0.0.0.0:9411->9411/tcp                                                                                 gokitconsul-zipkin
fcb3a785dd6c        consul:1.5.1                          "docker-entrypoint.s…"   10 minutes ago      Up 10 minutes       0.0.0.0:8400->8400/tcp, 8301-8302/udp, 0.0.0.0:8500->8500/tcp, 8300-8302/tcp, 8600/udp, 0.0.0.0:8600->8600/tcp   gokitconsul-consul

```

## Test

```sh
# sum
$ curl -X "POST" "https://localhost:8000/addsvc/sum" -H 'Content-Type: application/json; charset=utf-8' -d $'{ "a": 133, "b": 10333}'
{"rs":10466,"err":null}

# concat
$ curl -X "POST" "https://localhost:8000/addsvc/concat" -H 'Content-Type: application/json; charset=utf-8' -d $'{ "a": "133", "b": "10333"}'
{"rs":"13310333","err":null}

# foo
$ curl -X "POST" "https://localhost:8000/foosvc/foo" -H 'Content-Type: application/json; charset=utf-8' -d $'{ "s": "😆"}'
{"res":"😆","err":null}

$ curl -X "POST" "https://localhost:8000/foosvc/foo" -H 'Content-Type: application/json; charset=utf-8' -d $'{ "s": "hello gokit 😆"}'
{"error":"result exceeds maximum size"}
```

## Consul & zipkin

_consult_
visit http://localhost:8500
![Consul](./screenshots/consul.jpg)

_zipkin_
visit http://localhost:9411
![zipkin success](./screenshots/zipkin.jpg)

![zipkin bad request](./screenshots/zipkin2.jpg)

## Stop

```sh
# docker-compose down
$ make d
```

## Author

👤 **KAI-CHU CHUNG**

* Twitter: [@CageChung](https://twitter.com/CageChung)
* Github: [@cage1016](https://github.com/cage1016)

## 🤝 Contributing

Contributions, issues and feature requests are welcome!<br />Feel free to check [issues page](https://github.com/cage1016/gokitconsul/issues).

## Show your support

Give a ⭐️ if this project helped you!

## 📝 License

Copyright © 2019 [KAI-CHU CHUNG](https://github.com/cage1016).<br />
This project is [MIT](https://github.com/cage1016/gokitconsul/blob/master/LICENSE) licensed.

***
_This README was generated with ❤️ by [readme-md-generator](https://github.com/kefranabg/readme-md-generator)_