package k8sutil

import (
	"context"
	corev1 "k8s.io/api/core/v1"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatefulSetControl interface {
	Get(string, string) (*appsv1.StatefulSet, error)
	List(map[string]string) (*appsv1.StatefulSetList, error)
	Create(*appsv1.StatefulSet) error
	Update(*appsv1.StatefulSet) error
	Delete(*appsv1.StatefulSet) error
	DeleteByName(namespace, name string) error
	GetStatefulSetPodsByLabels(string, map[string]string) (*corev1.PodList, error)
	ListStatefulSetByLabels(string, map[string]string) (*appsv1.StatefulSetList, error)
}

type statefulSetController struct {
	client client.Client
}

func NewStatefulSetController(c client.Client) StatefulSetControl {
	return &statefulSetController{client: c}
}

func (c *statefulSetController) Get(namespace, name string) (*appsv1.StatefulSet, error) {
	sts := &appsv1.StatefulSet{}
	err := c.client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, sts)
	if err != nil {
		return nil, err
	}
	return sts, nil
}

func (c *statefulSetController) List(labels map[string]string) (*appsv1.StatefulSetList, error) {
	ss := &appsv1.StatefulSetList{}
	err := c.client.List(context.TODO(), ss, client.MatchingLabels(labels))
	if err != nil {
		return nil, err
	}
	return ss, nil
}

func (c *statefulSetController) Create(sts *appsv1.StatefulSet) error {
	return c.client.Create(context.TODO(), sts)
}

func (c *statefulSetController) Update(sts *appsv1.StatefulSet) error {
	return c.client.Update(context.TODO(), sts)
}

func (c *statefulSetController) Delete(sts *appsv1.StatefulSet) error {
	return c.client.Delete(context.TODO(), sts)
}

func (s *statefulSetController) DeleteByName(namespace, name string) error {
	sts, err := s.Get(namespace, name)
	if err != nil {
		return err
	}
	return s.Delete(sts)
}

func (c *statefulSetController) GetStatefulSetPodsByLabels(namespace string, labels map[string]string) (*corev1.PodList, error) {
	pods := &corev1.PodList{}
	if err := c.client.List(context.TODO(), pods, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		return nil, err
	}
	return pods, nil
}

func (s *statefulSetController) ListStatefulSetByLabels(namespace string, labels map[string]string) (*appsv1.StatefulSetList, error) {
	foundSts := &appsv1.StatefulSetList{}
	err := s.client.List(context.TODO(), foundSts, client.InNamespace(namespace), client.MatchingLabels(labels))
	return foundSts, err
}
