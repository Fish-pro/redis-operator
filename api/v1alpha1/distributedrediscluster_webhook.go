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

package v1alpha1

import (
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/Fish-pro/redis-operator/internal/helper"
)

const (
	minMasterSize       = 3
	minClusterReplicas  = 1
	defaultRedisImage   = "redis:5.0.4-alpine"
	defaultMonitorImage = "oliver006/redis_exporter:latest"
)

// log is for logging in this package.
var distributedredisclusterlog = logf.Log.WithName("distributedrediscluster-resource")

func (r *DistributedRedisCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

//+kubebuilder:webhook:path=/mutate-redis-run-v1alpha1-distributedrediscluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=redis.run,resources=distributedredisclusters,verbs=create;update,versions=v1alpha1,name=mdistributedrediscluster.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &DistributedRedisCluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *DistributedRedisCluster) Default() {
	distributedredisclusterlog.Info("default", "name", r.Name)

	if r.Spec.MasterSize < minMasterSize {
		r.Spec.MasterSize = minMasterSize
	}

	if r.Spec.Image == "" {
		r.Spec.Image = defaultRedisImage
	}

	if r.Spec.ServiceName == "" {
		r.Spec.ServiceName = r.Name
	}

	if r.Spec.Resources == nil || r.Spec.Resources.Size() == 0 {
		r.Spec.Resources = defaultResource()
	}

	mon := r.Spec.Monitor
	if mon != nil {
		if mon.Image == "" {
			mon.Image = defaultMonitorImage
		}

		if mon.Prometheus == nil {
			mon.Prometheus = &PrometheusSpec{}
		}
		if mon.Prometheus.Port == 0 {
			mon.Prometheus.Port = PrometheusExporterPortNumber
		}
		if r.Spec.Annotations == nil {
			r.Spec.Annotations = make(map[string]string)
		}

		r.Spec.Annotations["prometheus.io/scrape"] = "true"
		r.Spec.Annotations["prometheus.io/path"] = PrometheusExporterTelemetryPath
		r.Spec.Annotations["prometheus.io/port"] = fmt.Sprintf("%d", mon.Prometheus.Port)
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-redis-run-v1alpha1-distributedrediscluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=redis.run,resources=distributedredisclusters,verbs=create;update,versions=v1alpha1,name=vdistributedrediscluster.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &DistributedRedisCluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DistributedRedisCluster) ValidateCreate() (admission.Warnings, error) {
	distributedredisclusterlog.Info("validate create", "name", r.Name)

	if errs := validation.IsDNS1035Label(r.Spec.ServiceName); len(r.Spec.ServiceName) > 0 && len(errs) > 0 {
		return nil, fmt.Errorf("the custom service is invalid: invalid value: %s, %s", r.Spec.ServiceName, strings.Join(errs, ","))
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DistributedRedisCluster) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	distributedredisclusterlog.Info("validate update", "name", r.Name)

	if errs := validation.IsDNS1035Label(r.Spec.ServiceName); len(r.Spec.ServiceName) > 0 && len(errs) > 0 {
		return nil, fmt.Errorf("the custom service is invalid: invalid value: %s, %s", r.Spec.ServiceName, strings.Join(errs, ","))
	}

	oldObj, ok := old.(*DistributedRedisCluster)
	if !ok {
		err := fmt.Errorf("invalid obj type")
		distributedredisclusterlog.Error(err, "can not reflect type")
		return nil, err
	}
	if oldObj.Status.Status == "" {
		return nil, nil
	}
	if compareObj(r, oldObj) && oldObj.Status.Status != ClusterStatusOK {
		return nil, fmt.Errorf("redis cluster status: [%s], wait for the status to become %s before operating", oldObj.Status.Status, ClusterStatusOK)
	}

	return nil, nil
}

func compareObj(new, old *DistributedRedisCluster) bool {
	if helper.CompareInt32("MasterSize", new.Spec.MasterSize, old.Spec.MasterSize, distributedredisclusterlog) {
		return true
	}

	if helper.CompareStringValue("Image", new.Spec.Image, old.Spec.Image, distributedredisclusterlog) {
		return true
	}

	if !reflect.DeepEqual(new.Spec.Resources, old.Spec.Resources) {
		distributedredisclusterlog.Info("compare resource", "new", new.Spec.Resources, "old", old.Spec.Resources)
		return true
	}

	if !reflect.DeepEqual(new.Spec.PasswordSecret, old.Spec.PasswordSecret) {
		distributedredisclusterlog.Info("compare password", "new", new.Spec.PasswordSecret, "old", old.Spec.PasswordSecret)
		return true
	}

	return false
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DistributedRedisCluster) ValidateDelete() (admission.Warnings, error) {
	distributedredisclusterlog.Info("validate delete", "name", r.Name)

	return nil, nil
}
