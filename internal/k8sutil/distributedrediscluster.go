package k8sutil

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redisv1alpha1 "github.com/Fish-pro/redis-operator/api/v1alpha1"
)

type DistributedRedisClusterController interface {
	Get(string, string) *redisv1alpha1.DistributedRedisCluster
	Update(*redisv1alpha1.DistributedRedisCluster) error
	UpdateStatus(*redisv1alpha1.DistributedRedisCluster) error
}

type distributedRedisClusterControl struct {
	client client.Client
}

func NewDistributedRedisClusterController(c client.Client) DistributedRedisClusterController {
	return &distributedRedisClusterControl{client: c}
}

func (r *distributedRedisClusterControl) Get(namespace, name string) *redisv1alpha1.DistributedRedisCluster {
	cluster := &redisv1alpha1.DistributedRedisCluster{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, cluster)
	if err != nil {
		return nil
	}
	return cluster
}

func (r *distributedRedisClusterControl) Update(cluster *redisv1alpha1.DistributedRedisCluster) error {
	return r.client.Update(context.TODO(), cluster)
}

func (r *distributedRedisClusterControl) UpdateStatus(cluster *redisv1alpha1.DistributedRedisCluster) error {
	return r.client.Status().Update(context.TODO(), cluster)
}
