package pkg

import (
	"context"
	v17 "k8s.io/api/core/v1"
	v15 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v16 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	v13 "k8s.io/client-go/informers/core/v1"
	v14 "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
	v1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"log"
	"reflect"
	"time"
)

const workerNum = 5
const maxReties = 10

type controller struct {
	client        kubernetes.Interface
	ingressLister v1.IngressLister
	serviceLister v12.ServiceLister
	queue         workqueue.RateLimitingInterface
}

func (c controller) addService(obj interface{}) {
	c.enqueue(obj)
}

func (c controller) updateService(oldObj interface{}, newObj interface{}) {
	// 新旧对象一致则无需处理
	if reflect.DeepEqual(oldObj, newObj) {
		return
	}

	c.enqueue(newObj)
}

func (c controller) deleteService(obj interface{}) {

}

func (c controller) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		log.Print("key not found")
		runtime.HandleError(err)
	}
	c.queue.Add(key)
}

func (c controller) deleteIngress(obj interface{}) {
	ingress := obj.(*v15.Ingress)
	reference := v16.GetControllerOf(ingress)
	if reference == nil {
		return
	}

	if reference.Kind != "Service" {
		return
	}

	c.queue.Add(ingress.Namespace + "/" + ingress.Name)
}

func (c controller) Run(stopCh chan struct{}) {

	for i := 0; i < workerNum; i++ {
		go wait.Until(c.work, time.Minute, stopCh)
	}
	<-stopCh
}

func (c controller) work() {
	for c.processNextItem() {

	}
}

func (c controller) processNextItem() bool {
	item, shutdown := c.queue.Get()
	if shutdown {
		return false
	}

	defer c.queue.Done(item)

	key := item.(string)
	err := c.syncService(key)
	if err != nil {
		c.handleError(key, err)
		return false
	}
	return true
}

func (c controller) syncService(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	service, err := c.serviceLister.Services(namespace).Get(name)
	if errors.IsNotFound(err) {
		// service已删除 无需处理
		return nil
	}
	if err != nil {
		return err
	}

	// 新增及删除的情况
	_, ok := service.GetAnnotations()["ingress/http"]
	ingress, err := c.ingressLister.Ingresses(namespace).Get(name)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// 若当前service有ingress/http注解，但未创建ingress则创建
	// 同时 ingress 被删除后也会重建
	if ok && errors.IsNotFound(err) {
		//  创建 ingress
		igs := c.constructIngress(service)
		_, err := c.client.NetworkingV1().Ingresses(namespace).
			Create(context.TODO(), igs, v16.CreateOptions{})
		if err != nil {
			return err
		}
		log.Printf("ingress creat successful for %s in namespace %s\n",
			name, namespace)
		// 若当前service的注解ingress/http被移除，同时已存在ingress则删除该ingress
	} else if !ok && ingress != nil {
		//  删除 ingress
		err := c.client.NetworkingV1().Ingresses(namespace).
			Delete(context.TODO(), name, v16.DeleteOptions{})
		if err != nil {
			return err
		}
		log.Printf("ingress delete successful for %s in namespace %s\n",
			name, namespace)
	}

	return nil
}

func (c controller) handleError(key string, err error) {
	// 判断key的入队次数, 也即重试次数
	if c.queue.NumRequeues(key) <= maxReties {
		// 增加到限速队列, 让其后续再处理
		c.queue.AddRateLimited(key)
		return
	}

	runtime.HandleError(err)
	c.queue.Forget(key)
}

/*
//  apiVersion: networking.k8s.io/v1beta1
//  kind: Ingress
//  metadata:
//    name: ingress-service
//  spec:
//    ingressClassName: nginx
//    rules:
//    - host: myservice.foo.org
//      http:
//        paths:
//        - path: /
//          pathType: Prefix
//          backend:
//            service:
//               name: myservice
//               port:
//                 number: 80
*/
func (c controller) constructIngress(service *v17.Service) *v15.Ingress {
	ingress := v15.Ingress{}

	ingress.ObjectMeta.OwnerReferences = []v16.OwnerReference{
		*v16.NewControllerRef(service, v16.SchemeGroupVersion.WithKind("Service")),
	}
	ingress.Name = service.Name
	ingress.Namespace = service.Namespace
	pathType := v15.PathTypePrefix
	ingressClassName := "nginx"
	ingress.Spec = v15.IngressSpec{
		IngressClassName: &ingressClassName,
		Rules: []v15.IngressRule{
			{
				Host: "ingressx.com",
				IngressRuleValue: v15.IngressRuleValue{
					HTTP: &v15.HTTPIngressRuleValue{
						Paths: []v15.HTTPIngressPath{
							{
								Path:     "/",
								PathType: &pathType,
								Backend: v15.IngressBackend{
									Service: &v15.IngressServiceBackend{
										Name: service.Name,
										Port: v15.ServiceBackendPort{
											Number: 80,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return &ingress
}

func NewController(client kubernetes.Interface,
	serviceInformer v13.ServiceInformer,
	ingressInformer v14.IngressInformer) controller {
	c := controller{client: client,
		ingressLister: ingressInformer.Lister(),
		serviceLister: serviceInformer.Lister(),
		queue:         workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addService,
		UpdateFunc: c.updateService,
		DeleteFunc: c.deleteService,
	})

	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: c.deleteIngress,
	})
	return c
}
