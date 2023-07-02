package main

import (
	"github.com/linky/test/pkg"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"log"
)

func main() {
	// 1. 获取集群配置config
	// 2. 通过配置config获取客户端client
	// 3. 根据client获取informer
	// 4. 根据informer创建factory 并增加时间处理 event handler
	// 5. 启动informer, informer.Start()

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

	factory := informers.NewSharedInformerFactory(clientSet, 0)
	serviceInformer := factory.Core().V1().Services()
	ingressInformer := factory.Networking().V1().Ingresses()

	controller := pkg.NewController(clientSet, serviceInformer, ingressInformer)
	stopCh := make(chan struct{})
	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)
	log.Println("ingress operator started now")
	controller.Run(stopCh)
}
