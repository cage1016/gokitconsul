<h1 align="center">Welcome to gokitconsul 👋</h1>
<p>
  <a href="https://github.com/cage1016/gokitconsul/blob/master/LICENSE">
    <img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-yellow.svg" target="_blank" />
  </a>
  <a href="https://twitter.com/CageChung">
    <img alt="Twitter: CageChung" src="https://img.shields.io/twitter/follow/CageChung.svg?style=social" target="_blank" />
  </a>
</p>

> go kit microservice demo with consul

## Dependency

- consul as discover service
- zipkin as trace service
- gateway

## Services

- pkg/addsvc


## Install

```sh
make dockers
```

## Usage

```sh
# start
$ make u
docker-compose -f deployments/docker/docker-compose.yaml up -d
Creating network "docker_gokitconsul_base_net" with driver "bridge"
Creating gokitconsul-consul ... done
Creating gokitconsul-addsvc ... done
Creating gokitconsul-gateway ... done


$ docker ps
CONTAINER ID        IMAGE                                COMMAND                  CREATED             STATUS              PORTS                                                        NAMES
eb5c3799e481        cage1016/gokitconsul-gateway:latest   "/exe"                   30 seconds ago      Up 29 seconds       0.0.0.0:9000->9000/tcp                                       gokitconsul-gateway
a3ec4fe2a9f3        cage1016/gokitconsul-addsvc:latest    "/exe"                   31 seconds ago      Up 29 seconds                                                                    gokitconsul-addsvc
16ac843b5026        consul:1.5.1                         "docker-entrypoint.s…"   32 seconds ago      Up 30 seconds       8300-8302/tcp, 8500/tcp, 8301-8302/udp, 8600/tcp, 8600/udp   gokitconsul-consul
```

## Test

```sh
# sum
$ curl -X "POST" "https://localhost:8000/addsvc/sum" -H 'Content-Type: application/json; charset=utf-8' -d $'{ "a": 133, "b": 10333}'
{"v":10466}

# concat
$ curl -X "POST" "https://localhost:8000/addsvc/concat" -H 'Content-Type: application/json; charset=utf-8' -d $'{ "a": "133", "b": "10333"}'
{"v":"13310333"}

```

## Stop

```sh
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