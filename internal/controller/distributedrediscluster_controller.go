/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	redisv1alpha1 "github.com/Fish-pro/redis-operator/api/v1alpha1"
	"github.com/Fish-pro/redis-operator/internal/config"
	"github.com/Fish-pro/redis-operator/internal/controller/clustering"
	"github.com/Fish-pro/redis-operator/internal/controller/heal"
	"github.com/Fish-pro/redis-operator/internal/helper"
	"github.com/Fish-pro/redis-operator/internal/k8sutil"
	"github.com/Fish-pro/redis-operator/internal/redisutil"
	"github.com/Fish-pro/redis-operator/internal/resources/statefulset"
)

// DistributedRedisClusterReconciler reconciles a DistributedRedisCluster object
type DistributedRedisClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	StsControl  k8sutil.StatefulSetControl
	SvcControl  k8sutil.ServiceControl
	CmControl   k8sutil.ConfigMapControl
	PvcControl  k8sutil.PvcControl
	SelfControl k8sutil.DistributedRedisClusterController
}

type syncContext struct {
	cluster      *redisv1alpha1.DistributedRedisCluster
	clusterInfos *redisutil.ClusterInfos
	admin        redisutil.IAdmin
	healer       IHeal
	pods         *corev1.PodList
	reqLogger    logr.Logger
}

//+kubebuilder:rbac:groups=redis.run,resources=distributedredisclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=redis.run,resources=distributedredisclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=redis.run,resources=distributedredisclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DistributedRedisCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *DistributedRedisClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	cluster := &redisv1alpha1.DistributedRedisCluster{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{Requeue: true}, err
		}
		return ctrl.Result{}, nil
	}

	if !cluster.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	syncCtx := &syncContext{
		cluster:   cluster,
		reqLogger: logger,
	}

	if err := r.ensureCluster(ctx, cluster); err != nil {
		logger.Error(err, "failed to ensure cluster,retry after 10s")
		newStatus := cluster.Status.DeepCopy()
		SetClusterScaling(newStatus, err.Error())
		r.updateClusterIfNeed(cluster, newStatus, logger)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	matchLabels := getLabels(cluster)
	stsList, err := r.StsControl.List(matchLabels)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	podList, err := r.StsControl.GetStatefulSetPodsByLabels(cluster.Namespace, matchLabels)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	syncCtx.pods = podList
	if !r.waitStsReady(stsList) || !r.waitPodReady(podList) {
		logger.V(6).Info("wait for statefulSets ready, retry after 10s")
		newStatus := cluster.Status.DeepCopy()
		SetClusterScaling(newStatus, "statefulSet is reconciling, retry after 10s")
		r.updateClusterIfNeed(cluster, newStatus, logger)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	password, err := statefulset.GetClusterPassword(r.Client, cluster)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	admin, err := newRedisAdmin(podList, password, config.RedisConf(), logger)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	syncCtx.admin = admin

	clusterInfos, err := admin.GetClusterInfos()
	if err != nil {
		if clusterInfos.Status == redisutil.ClusterInfosPartial {
			return ctrl.Result{}, err
		}
	}
	syncCtx.clusterInfos = clusterInfos

	syncCtx.healer = NewHealer(&heal.CheckAndHeal{
		Logger:     logger,
		PodControl: k8sutil.NewPodController(r.Client),
		Pods:       podList,
		DryRun:     false,
	})

	requeue, err := syncCtx.healer.Heal(cluster, clusterInfos, admin)
	if err != nil {
		return ctrl.Result{}, err
	}
	if requeue {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	err = r.waitForClusterJoin(syncCtx)
	if err != nil {
		newStatus := cluster.Status.DeepCopy()
		SetClusterFailed(newStatus, err.Error())
		r.updateClusterIfNeed(cluster, newStatus, logger)
		return ctrl.Result{}, err
	}

	if err := admin.SetConfigIfNeed(cluster.Spec.Config); err != nil {
		return ctrl.Result{}, err
	}

	status := buildClusterStatus(clusterInfos, syncCtx.pods, cluster, logger)
	if is := r.isScalingDown(cluster, logger); is {
		SetClusterRebalancing(status, "scaling down")
	}
	logger.V(6).Info("buildClusterStatus", "status", status)
	r.updateClusterIfNeed(cluster, status, logger)

	cluster.Status = *status
	if needClusterOperation(cluster, logger) {
		logger.Info(">>>>>> clustering")
		err = r.syncCluster(syncCtx)
		if err != nil {
			newStatus := cluster.Status.DeepCopy()
			SetClusterFailed(newStatus, err.Error())
			r.updateClusterIfNeed(cluster, newStatus, logger)
			return ctrl.Result{}, err
		}
	}

	newClusterInfos, err := admin.GetClusterInfos()
	if err != nil {
		if clusterInfos.Status == redisutil.ClusterInfosPartial {
			return ctrl.Result{}, err
		}
	}
	newStatus := buildClusterStatus(newClusterInfos, syncCtx.pods, cluster, logger)
	SetClusterOK(newStatus, "OK")
	r.updateClusterIfNeed(cluster, newStatus, logger)
	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

func (r *DistributedRedisClusterReconciler) syncCluster(ctx *syncContext) error {
	cluster := ctx.cluster
	admin := ctx.admin
	clusterInfos := ctx.clusterInfos
	expectMasterNum := cluster.Spec.MasterSize
	rCluster, nodes, err := newRedisCluster(clusterInfos, cluster)
	if err != nil {
		return err
	}
	clusterCtx := clustering.NewCtx(rCluster, nodes, cluster.Spec.MasterSize, cluster.Name, ctx.reqLogger)
	if err := clusterCtx.DispatchMasters(); err != nil {
		return err
	}
	curMasters := clusterCtx.GetCurrentMasters()
	newMasters := clusterCtx.GetNewMasters()
	ctx.reqLogger.Info("masters", "newMasters", len(newMasters), "curMasters", len(curMasters))
	if len(curMasters) == 0 {
		ctx.reqLogger.Info("Creating cluster")
		if err := clusterCtx.PlaceSlaves(); err != nil {
			return err

		}
		if err := clusterCtx.AttachingSlavesToMaster(admin); err != nil {
			return err
		}

		if err := clusterCtx.AllocSlots(admin, newMasters); err != nil {
			return err
		}
	} else if len(newMasters) > len(curMasters) {
		ctx.reqLogger.Info("Scaling up")
		if err := clusterCtx.PlaceSlaves(); err != nil {
			return err

		}
		if err := clusterCtx.AttachingSlavesToMaster(admin); err != nil {
			return err
		}

		if err := clusterCtx.RebalancedCluster(admin, newMasters); err != nil {
			return err
		}
	} else if cluster.Status.MinReplicationFactor < cluster.Spec.ClusterReplicas {
		ctx.reqLogger.Info("Scaling slave")
		if err := clusterCtx.PlaceSlaves(); err != nil {
			return err

		}
		if err := clusterCtx.AttachingSlavesToMaster(admin); err != nil {
			return err
		}
	} else if len(curMasters) > int(expectMasterNum) {
		ctx.reqLogger.Info("Scaling down")
		var allMaster redisutil.Nodes
		allMaster = append(allMaster, newMasters...)
		allMaster = append(allMaster, curMasters...)
		if err := clusterCtx.DispatchSlotToNewMasters(admin, newMasters, curMasters, allMaster); err != nil {
			return err
		}
		if err := r.scalingDown(ctx, len(curMasters), clusterCtx.GetStatefulsetNodes()); err != nil {
			return err
		}
	}
	return nil
}

func (r *DistributedRedisClusterReconciler) isScalingDown(cluster *redisv1alpha1.DistributedRedisCluster, reqLogger logr.Logger) bool {
	stsList, err := r.StsControl.ListStatefulSetByLabels(cluster.Namespace, getLabels(cluster))
	if err != nil {
		reqLogger.Error(err, "ListStatefulSetByLabels")
		return false
	}
	if len(stsList.Items) > int(cluster.Spec.MasterSize) {
		return true
	}
	return false
}

func (r *DistributedRedisClusterReconciler) scalingDown(ctx *syncContext, currentMasterNum int, statefulSetNodes map[string]redisutil.Nodes) error {
	cluster := ctx.cluster
	SetClusterRebalancing(&cluster.Status,
		fmt.Sprintf("scale down, currentMasterSize: %d, expectMasterSize %d", currentMasterNum, cluster.Spec.MasterSize))
	r.SelfControl.UpdateStatus(cluster)
	admin := ctx.admin
	expectMasterNum := int(cluster.Spec.MasterSize)
	for i := currentMasterNum - 1; i >= expectMasterNum; i-- {
		stsName := statefulset.ClusterStatefulSetName(cluster.Name, i)
		for _, node := range statefulSetNodes[stsName] {
			admin.Connections().Remove(node.IPPort())
		}
	}
	for i := currentMasterNum - 1; i >= expectMasterNum; i-- {
		stsName := statefulset.ClusterStatefulSetName(cluster.Name, i)
		ctx.reqLogger.Info("scaling down", "statefulSet", stsName)
		sts, err := r.StsControl.Get(cluster.Namespace, stsName)
		if err != nil {
			return err
		}
		for _, node := range statefulSetNodes[stsName] {
			ctx.reqLogger.Info("forgetNode", "id", node.ID, "ip", node.IP, "role", node.GetRole())
			if len(node.Slots) > 0 {
				return err
			}
			if err := admin.ForgetNode(node.ID); err != nil {
				return err
			}
		}
		// remove resource
		if err := r.StsControl.DeleteByName(cluster.Namespace, stsName); err != nil {
			ctx.reqLogger.Error(err, "DeleteStatefulSetByName", "statefulSet", stsName)
		}
		svcName := statefulset.ClusterHeadlessSvcName(cluster.Name, i)
		if err := r.SvcControl.DeleteByName(cluster.Namespace, svcName); err != nil {
			ctx.reqLogger.Error(err, "DeleteServiceByName", "service", svcName)
		}
		if err := r.PvcControl.DeletePvcByLabels(cluster.Namespace, sts.Labels); err != nil {
			ctx.reqLogger.Error(err, "DeletePvcByLabels", "labels", sts.Labels)
		}
		// wait pod Terminating
		waiter := &waitPodTerminating{
			name:                  "waitPodTerminating",
			statefulSet:           stsName,
			timeout:               30 * time.Second * time.Duration(cluster.Spec.ClusterReplicas+2),
			tick:                  5 * time.Second,
			statefulSetController: r.StsControl,
			cluster:               cluster,
		}
		if err := waiting(waiter, ctx.reqLogger); err != nil {
			ctx.reqLogger.Error(err, "waitPodTerminating")
		}
	}
	return nil
}

func (r *DistributedRedisClusterReconciler) waitForClusterJoin(ctx *syncContext) error {
	if infos, err := ctx.admin.GetClusterInfos(); err == nil {
		ctx.reqLogger.V(6).Info("debug waitForClusterJoin", "cluster infos", infos)
		return nil
	}
	var firstNode *redisutil.Node
	for _, nodeInfo := range ctx.clusterInfos.Infos {
		firstNode = nodeInfo.Node
		break
	}
	ctx.reqLogger.Info(">>> Sending CLUSTER MEET messages to join the cluster")
	err := ctx.admin.AttachNodeToCluster(firstNode.IPPort())
	if err != nil {
		return err
	}
	// Give one second for the join to start, in order to avoid that
	// waiting for cluster join will find all the nodes agree about
	// the config as they are still empty with unassigned slots.
	time.Sleep(1 * time.Second)

	_, err = ctx.admin.GetClusterInfos()
	if err != nil {
		return err
	}
	return nil
}

func (r *DistributedRedisClusterReconciler) waitStsReady(stsList *appsv1.StatefulSetList) bool {
	for _, sts := range stsList.Items {
		if *(sts.Spec.Replicas) != sts.Status.AvailableReplicas {
			return false
		}
	}
	return true
}

func (r *DistributedRedisClusterReconciler) waitPodReady(podList *corev1.PodList) bool {
	for _, pod := range podList.Items {
		if !helper.IsPodReady(&pod) {
			return false
		}
	}
	return true
}

func (r *DistributedRedisClusterReconciler) updateClusterIfNeed(cluster *redisv1alpha1.DistributedRedisCluster,
	newStatus *redisv1alpha1.DistributedRedisClusterStatus,
	reqLogger logr.Logger) {
	if compareStatus(&cluster.Status, newStatus, reqLogger) {
		reqLogger.WithValues("namespace", cluster.Namespace, "name", cluster.Name).
			V(3).Info("status changed")
		cluster.Status = *newStatus
		r.SelfControl.UpdateStatus(cluster)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DistributedRedisClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&redisv1alpha1.DistributedRedisCluster{}).
		Complete(r)
}
