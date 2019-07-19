package grpcsr

import (
	"fmt"
	"net"
	"time"

	"github.com/go-kit/kit/log"
	consulsd "github.com/go-kit/kit/sd/consul"
	"github.com/hashicorp/consul/api"
)

type ConsulRegister struct {
	ConsulAddress                  string   // consul address
	ServiceName                    string   // service name
	Tags                           []string // consul tags
	ServicePort                    int      //service port
	MetricsPort                    int      //service port
	DeregisterCriticalServiceAfter time.Duration
	Interval                       time.Duration
	logger                         log.Logger
}

func NewConsulRegister(consulAddress, serviceName string, servicePort, metricsPort int, tags []string, logger log.Logger) *ConsulRegister {
	return &ConsulRegister{
		ConsulAddress:                  consulAddress,
		ServiceName:                    serviceName,
		Tags:                           tags,
		ServicePort:                    servicePort,
		MetricsPort:                    metricsPort,
		DeregisterCriticalServiceAfter: time.Duration(1) * time.Minute,
		Interval:                       time.Duration(10) * time.Second,
		logger:                         logger,
	}
}

// https://github.com/ru-rocker/gokit-playground/blob/master/lorem-consul/register.go
// https://github.com/hatlonely/hellogolang/blob/master/sample/addservice/internal/grpcsr/consul_register.go
func (r *ConsulRegister) NewConsulGRPCRegister() (*consulsd.Registrar, error) {
	consulConfig := api.DefaultConfig()
	consulConfig.Address = r.ConsulAddress
	consulClient, err := api.NewClient(consulConfig)
	if err != nil {
		return nil, err
	}
	client := consulsd.NewClient(consulClient)

	IP := localIP()
	reg := &api.AgentServiceRegistration{
		ID:   fmt.Sprintf("%v-%v-%v", r.ServiceName, IP, r.ServicePort),
		Name: fmt.Sprintf("grpc.health.v1.%v", r.ServiceName),
		Tags: r.Tags,
		Port: r.ServicePort,
		Meta: map[string]string{
			"metricsport": fmt.Sprintf("%v", r.MetricsPort),
		},
		Address: IP,
		Check: &api.AgentServiceCheck{
			Interval:                       r.Interval.String(),
			GRPC:                           fmt.Sprintf("%v:%v/%v", IP, r.ServicePort, r.ServiceName),
			DeregisterCriticalServiceAfter: r.DeregisterCriticalServiceAfter.String(),
		},
	}
	return consulsd.NewRegistrar(client, reg, r.logger), nil
}

func localIP() (s0 string) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
