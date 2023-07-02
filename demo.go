package main

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"log"
	"time"
)

func mainb() {
	// config, 若 controller 运行在集群内部则无法找到config, 只能通过 rest.InClusterConfig() 获取
	// InClusterConfig是针对 k8s 基础组件以pod方式运行在k8s上的环境
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		clusterConfig, err := rest.InClusterConfig()
		if err != nil {
			log.Fatalln("can't get k8s cluster config")
		}
		config = clusterConfig
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalln("can't create client")
	}

	// 查看指定 namespace 的pod
	list, err := clientSet.CoreV1().Pods("kube-system").
		List(context.Background(), metav1.ListOptions{})
	fmt.Println("=====> pods:")
	for index, item := range list.Items {
		fmt.Printf("%d %s\n", index+1, item.Name)
	}
	// 增加限速队列
	//queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "controller")

	limitingQueue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	//workqueue.NewRateLimitingQueueWithConfig(workqueue.DefaultControllerRateLimiter())

	// get informer
	factory := informers.NewSharedInformerFactory(clientSet, 0)
	informer := factory.Core().V1().Pods().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			currentTime := time.Now()
			// 2006-01-02 15:04:05.000 是Go语言诞生时间
			fmt.Printf("add event, time: %s\n", currentTime.Format("2006-01-02 15:04:05.000"))

			addToLimitQueue(obj, limitingQueue)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			currentTime := time.Now()
			fmt.Printf("upate event, time: %s\n", currentTime.Format("2006-01-02 15:04:05.000"))

			addToLimitQueue(newObj, limitingQueue)
		},
		DeleteFunc: func(obj interface{}) {
			currentTime := time.Now()
			fmt.Printf("delete event, time: %s\n", currentTime.Format("2006-01-02 15:04:05.000"))

			addToLimitQueue(obj, limitingQueue)
		},
	})
	stopCh := make(chan struct{})
	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)
	<-stopCh
}

/*
添加限速队列
*/
func addToLimitQueue(newObj interface{}, limitingQueue workqueue.RateLimitingInterface) {
	key, err := cache.MetaNamespaceKeyFunc(newObj)
	if err != nil {
		panic(err)
	}
	limitingQueue.AddRateLimited(key)
}
