package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/log"

	redisv1alpha1 "github.com/Fish-pro/redis-operator/api/v1alpha1"
	"github.com/Fish-pro/redis-operator/internal/resources/configmap"
	"github.com/Fish-pro/redis-operator/internal/resources/service"
	"github.com/Fish-pro/redis-operator/internal/resources/statefulset"
)

func (r *DistributedRedisClusterReconciler) ensureCluster(ctx context.Context, cluster *redisv1alpha1.DistributedRedisCluster) error {
	logger := log.FromContext(ctx)
	labels := getLabels(cluster)
	if err := r.ensureConfigMap(ctx, cluster, labels); err != nil {
		logger.Error(err, "failed to ensure configmap", "namespace", cluster.Namespace, "name", cluster.Name)
		return err
	} else {
		logger.V(6).Info("ensure configmap successfully", "namespace", cluster.Namespace, "name", cluster.Name)
	}

	if err := r.ensureStatefulSets(ctx, cluster, labels); err != nil {
		logger.Error(err, "failed to ensure statefulset", "namespace", cluster.Namespace, "name", cluster.Name)
		return err
	} else {
		logger.V(6).Info("ensure statefulset successfully", "namespace", cluster.Namespace, "name", cluster.Name)
	}

	if err := r.ensureHeadlessServices(ctx, cluster, labels); err != nil {
		logger.Error(err, "failed to ensure headless service", "namespace", cluster.Namespace, "name", cluster.Name)
		return err
	} else {
		logger.V(6).Info("ensure headless service successfully", "namespace", cluster.Namespace, "name", cluster.Name)
	}

	if err := r.ensureService(ctx, cluster, labels); err != nil {
		logger.Error(err, "failed to ensure service", "namespace", cluster.Namespace, "name", cluster.Name)
		return err
	} else {
		logger.V(6).Info("ensure service successfully", "namespace", cluster.Namespace, "name", cluster.Name)
	}

	return nil
}

func (r *DistributedRedisClusterReconciler) ensureConfigMap(ctx context.Context, cluster *redisv1alpha1.DistributedRedisCluster, labels map[string]string) error {
	logger := log.FromContext(ctx)
	name := configmap.RedisConfigMapName(cluster.Name)
	cm, err := r.CmControl.Get(cluster.Namespace, name)
	if err != nil {
		if errors.IsNotFound(err) {
			newCm := configmap.NewConfigMapForCR(cluster, labels)
			return r.CmControl.Create(newCm)
		}
		logger.Error(err, "failed to get configmap", "namespace", cluster.Namespace, "configmap.name", name)
		return err
	}

	if isRedisConfChanged(cm.Data[configmap.RedisConfKey], cluster.Spec.Config, logger) {
		newCm := configmap.NewConfigMapForCR(cluster, labels)
		return r.CmControl.Update(newCm)
	}

	return nil
}

func (r *DistributedRedisClusterReconciler) ensureStatefulSets(ctx context.Context, cluster *redisv1alpha1.DistributedRedisCluster, labels map[string]string) error {
	for i := 0; i < int(cluster.Spec.MasterSize); i++ {
		name := statefulset.ClusterStatefulSetName(cluster.Name, i)
		svcName := statefulset.ClusterHeadlessSvcName(cluster.Spec.ServiceName, i)
		labels[redisv1alpha1.StatefulSetLabel] = name
		if err := r.ensureStatefulSet(ctx, cluster, name, svcName, labels); err != nil {
			return err
		}
	}
	return nil
}

func (r *DistributedRedisClusterReconciler) ensureStatefulSet(ctx context.Context, cluster *redisv1alpha1.DistributedRedisCluster, name string, svcName string, labels map[string]string) error {
	logger := log.FromContext(ctx)
	sts, err := r.StsControl.Get(cluster.Namespace, name)
	if err != nil {
		if errors.IsNotFound(err) {
			sts, err := statefulset.NewStatefulSetForCR(cluster, name, svcName, cluster.Spec.ClusterReplicas, labels)
			if err != nil {
				logger.Error(err, "failed to new statefulset", "namespace", cluster.Namespace, "statefulset.name", name)
				return err
			}
			return r.StsControl.Create(sts)
		}
		logger.Error(err, "filed to get statefulset", "namespace", cluster.Namespace, "statefulset.name", name)
		return err
	}
	if shouldUpdateRedis(cluster, sts) {
		sts, err := statefulset.NewStatefulSetForCR(cluster, name, svcName, cluster.Spec.ClusterReplicas, labels)
		if err != nil {
			logger.Error(err, "failed to new statefulset", "namespace", cluster.Namespace, "statefulset.name", name)
			return err
		}
		return r.StsControl.Update(sts)
	}
	return nil
}

func (r *DistributedRedisClusterReconciler) ensureService(ctx context.Context, cluster *redisv1alpha1.DistributedRedisCluster, labels map[string]string) error {
	logger := log.FromContext(ctx)
	svc, err := r.SvcControl.Get(cluster.Namespace, cluster.Spec.ServiceName)
	if err != nil {
		if errors.IsNotFound(err) {
			svc := service.NewSvcForCR(cluster, cluster.Spec.ServiceName, labels)
			return r.SvcControl.Create(svc)
		}
		logger.Error(err, "failed to get service", "namespace", cluster.Namespace, "service.name", cluster.Spec.ServiceName)
		return err
	}
	newSvc := service.NewSvcForCR(cluster, cluster.Spec.ServiceName, labels)
	newSvc.SetResourceVersion(svc.GetResourceVersion())
	return r.SvcControl.Update(newSvc)
}

func (r *DistributedRedisClusterReconciler) ensureHeadlessServices(ctx context.Context, cluster *redisv1alpha1.DistributedRedisCluster, labels map[string]string) error {
	for i := 0; i < int(cluster.Spec.MasterSize); i++ {
		name := statefulset.ClusterStatefulSetName(cluster.Name, i)
		svcName := statefulset.ClusterHeadlessSvcName(cluster.Spec.ServiceName, i)
		labels[redisv1alpha1.StatefulSetLabel] = name
		if err := r.ensureHeadlessService(ctx, cluster, svcName, labels); err != nil {
			return err
		}
	}
	return nil
}

func (r *DistributedRedisClusterReconciler) ensureHeadlessService(ctx context.Context, cluster *redisv1alpha1.DistributedRedisCluster, name string, labels map[string]string) error {
	logger := log.FromContext(ctx)
	svc, err := r.SvcControl.Get(cluster.Namespace, name)
	if err != nil {
		if errors.IsNotFound(err) {
			svc := service.NewHeadLessSvcForCR(cluster, name, labels)
			return r.SvcControl.Create(svc)
		}
		logger.Error(err, "failed to get service", "namespace", cluster.Namespace, "service.name", name)
		return err
	}

	newSvc := service.NewHeadLessSvcForCR(cluster, name, labels)
	newSvc.SetResourceVersion(svc.GetResourceVersion())
	return r.SvcControl.Update(newSvc)
}
