package k8sutil

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ServiceControl interface {
	Get(string, string) (*corev1.Service, error)
	Create(*corev1.Service) error
	Update(*corev1.Service) error
	Delete(*corev1.Service) error
	DeleteByName(string, string) error
}

type ServiceController struct {
	client client.Client
}

func NewServiceController(c client.Client) ServiceControl {
	return &ServiceController{client: c}
}

func (c *ServiceController) Get(namespace, name string) (*corev1.Service, error) {
	svc := &corev1.Service{}
	err := c.client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, svc)
	if err != nil {
		return nil, err
	}
	return svc, nil
}

func (c *ServiceController) Create(svc *corev1.Service) error {
	return c.client.Create(context.TODO(), svc)
}

func (c *ServiceController) Update(svc *corev1.Service) error {
	return c.client.Update(context.TODO(), svc)
}

func (c *ServiceController) Delete(svc *corev1.Service) error {
	return c.client.Delete(context.TODO(), svc)
}

func (c *ServiceController) DeleteByName(namespace, name string) error {
	svc, err := c.Get(namespace, name)
	if err != nil {
		return err
	}
	return c.Delete(svc)
}
