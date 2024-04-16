package k8sutil

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ConfigMapControl interface {
	Get(string, string) (*corev1.ConfigMap, error)
	Create(*corev1.ConfigMap) error
	Update(*corev1.ConfigMap) error
	Delete(*corev1.ConfigMap) error
}

type ConfigMapController struct {
	client client.Client
}

func NewConfigMapController(c client.Client) ConfigMapControl {
	return &ConfigMapController{client: c}
}

func (c *ConfigMapController) Get(namespace, name string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	err := c.client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, cm)
	if err != nil {
		return nil, err
	}
	return cm, nil
}

func (c *ConfigMapController) Create(cm *corev1.ConfigMap) error {
	return c.client.Create(context.TODO(), cm)
}

func (c *ConfigMapController) Update(cm *corev1.ConfigMap) error {
	return c.client.Update(context.TODO(), cm)
}

func (c *ConfigMapController) Delete(cm *corev1.ConfigMap) error {
	return c.client.Delete(context.TODO(), cm)
}
