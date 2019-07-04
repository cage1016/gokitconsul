package service

import (
	"context"

	"google.golang.org/grpc/health/grpc_health_v1"
)

// HealthImpl grpc 健康檢查
// https://studygolang.com/articles/18737
type HealthImpl struct {
}

// Check 實現健康檢查接口，這裏直接返回健康狀態，這裏也可以有更復雜的健康檢查策略，
// 比如根據服務器負載來返回
// https://github.com/hashicorp/consul/blob/master/agent/checks/grpc.go
// consul 檢查服務器的健康狀態，consul 用 google.golang.org/grpc/health/grpc_health_v1.HealthServer 接口，
// 實現了對 grpc健康檢查的支持，所以我們只需要實現先這個接口，consul 就能利用這個接口作健康檢查了
func (h *HealthImpl) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

// Watch HealthServer interface 有兩個方法
// Check(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error)
// Watch(*HealthCheckRequest, Health_WatchServer) error
// 所以在 HealthImpl 結構提不僅要實現 Check 方法，還要實現 Watch 方法
func (h *HealthImpl) Watch(req *grpc_health_v1.HealthCheckRequest, w grpc_health_v1.Health_WatchServer) error {
	return nil
}
